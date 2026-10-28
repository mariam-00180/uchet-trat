package main

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

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

	statements := []string{
		`CREATE TABLE IF NOT EXISTS expenses (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			category TEXT NOT NULL,
			amount REAL NOT NULL CHECK(amount > 0),
			expense_date TEXT NOT NULL DEFAULT (date('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE COLLATE NOCASE,
			password_hash TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			token_hash TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			expires_at TEXT NOT NULL
		)`,
	}
	for _, q := range statements {
		if _, err := db.Exec(q); err != nil {
			db.Close()
			return nil, err
		}
	}

	// Safe migration for the user's existing expenses table. Existing rows stay intact.
	columns, err := tableColumns(db, "expenses")
	if err != nil {
		db.Close()
		return nil, err
	}
	if !columns["expense_date"] {
		if _, err := db.Exec(`ALTER TABLE expenses ADD COLUMN expense_date TEXT NOT NULL DEFAULT ''`); err != nil {
			db.Close()
			return nil, err
		}
	}
	if !columns["user_id"] {
		if _, err := db.Exec(`ALTER TABLE expenses ADD COLUMN user_id INTEGER NOT NULL DEFAULT 1`); err != nil {
			db.Close()
			return nil, err
		}
	}
	return db, nil
}

func tableColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var def sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &def, &pk); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
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
		amount, parseErr := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(parts[1]), ",", "."), 64)
		if category == "" || parseErr != nil || amount <= 0 {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO expenses(category, amount, expense_date, user_id) VALUES (?, ?, date('now'), 1)`, category, amount); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	fmt.Println("Legacy expenses imported when database was empty.")
	return nil
}
