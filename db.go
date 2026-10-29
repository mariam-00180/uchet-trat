package main

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func openDatabase() (*sql.DB, error) {
	path := envOr("DB_PATH", "expenses.db")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}

	if _, err := db.Exec(`PRAGMA busy_timeout = 5000; PRAGMA foreign_keys = ON;`); err != nil {
		db.Close()
		return nil, err
	}

	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("database migrations failed: %w", err)
	}

	return db, nil
}

// runMigrations executes embedded SQL files in filename order.
// Successfully applied migrations are recorded and never repeated.
func runMigrations(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT (datetime('now'))
		)
	`)
	if err != nil {
		return err
	}

	files, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return err
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Name() < files[j].Name()
	})

	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".sql") {
			continue
		}

		version := file.Name()

		var applied string
		err := db.QueryRow(
			`SELECT version FROM schema_migrations WHERE version = ?`,
			version,
		).Scan(&applied)

		if err == nil {
			continue
		}
		if err != sql.ErrNoRows {
			return err
		}

		content, err := migrationFiles.ReadFile("migrations/" + version)
		if err != nil {
			return err
		}

		tx, err := db.Begin()
		if err != nil {
			return err
		}

		statements := strings.Split(string(content), ";")

		for _, raw := range statements {
			statement := stripSQLComments(raw)
			if statement == "" {
				continue
			}

			if err := executeMigrationStatement(tx, statement); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf(
					"migration %s failed on %q: %w",
					version,
					statement,
					err,
				)
			}
		}

		if _, err := tx.Exec(
			`INSERT INTO schema_migrations(version) VALUES (?)`,
			version,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", version, err)
		}

		fmt.Printf("Applied migration: %s\n", version)
	}

	return nil
}

// Skip ALTER TABLE ... ADD COLUMN if the column already exists.
// This supports upgrades from databases created by earlier versions.
func executeMigrationStatement(tx *sql.Tx, statement string) error {
	normalized := strings.ToLower(
		strings.Join(strings.Fields(statement), " "),
	)
	fields := strings.Fields(normalized)

	if len(fields) >= 6 &&
		fields[0] == "alter" &&
		fields[1] == "table" &&
		fields[3] == "add" &&
		fields[4] == "column" {

		table := strings.Trim(fields[2], "`\"[]")
		column := strings.Trim(fields[5], "`\"[]")

		if !safeIdentifier(table) || !safeIdentifier(column) {
			return fmt.Errorf("invalid table or column name")
		}

		columns, err := tableColumnsFrom(tx, table)
		if err != nil {
			return err
		}

		if columns[column] {
			return nil
		}
	}

	_, err := tx.Exec(statement)
	return err
}

func stripSQLComments(statement string) string {
	lines := strings.Split(statement, "\n")
	kept := make([]string, 0, len(lines))

	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		kept = append(kept, line)
	}

	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func safeIdentifier(value string) bool {
	if value == "" {
		return false
	}

	for _, r := range value {
		if !((r >= 'a' && r <= 'z') ||
			(r >= '0' && r <= '9') ||
			r == '_') {
			return false
		}
	}

	return true
}

type sqlQueryer interface {
	Query(string, ...any) (*sql.Rows, error)
}

func tableColumnsFrom(q sqlQueryer, table string) (map[string]bool, error) {
	if !safeIdentifier(table) {
		return nil, fmt.Errorf("invalid table name")
	}

	rows, err := q.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns := make(map[string]bool)

	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var defaultValue sql.NullString

		if err := rows.Scan(
			&cid,
			&name,
			&typ,
			&notNull,
			&defaultValue,
			&pk,
		); err != nil {
			return nil, err
		}

		columns[strings.ToLower(name)] = true
	}

	return columns, rows.Err()
}

func tableColumns(db *sql.DB, table string) (map[string]bool, error) {
	return tableColumnsFrom(db, table)
}

func importOldExpenses(db *sql.DB, filename string) error {
	var count int

	if err := db.QueryRow(`SELECT COUNT(*) FROM expenses`).Scan(&count); err != nil {
		return err
	}

	if count > 0 {
		return nil
	}

	data, err := os.ReadFile(filename)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "\t", 2)
		if len(parts) != 2 {
			continue
		}

		category := strings.TrimSpace(parts[0])
		amount, parseErr := strconv.ParseFloat(
			strings.ReplaceAll(strings.TrimSpace(parts[1]), ",", "."),
			64,
		)

		if category == "" || parseErr != nil || amount <= 0 {
			continue
		}

		if _, err := tx.Exec(
			`INSERT INTO expenses(category, amount, expense_date, user_id)
			 VALUES (?, ?, date('now'), 1)`,
			category,
			amount,
		); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	fmt.Println("Legacy expenses imported when database was empty.")
	return nil
}
