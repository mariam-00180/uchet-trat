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
	db, err := sql.Open("sqlite", "expenses.db")
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}

	query := `
	CREATE TABLE IF NOT EXISTS expenses (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		category TEXT NOT NULL,
		amount REAL NOT NULL,
		expense_date TEXT NOT NULL DEFAULT (date('now'))
	);`

	if _, err := db.Exec(query); err != nil {
		db.Close()
		return nil, err
	}

	// Add the date column to an existing database if it is missing.
	var dateColumnExists bool

	rows, err := db.Query("PRAGMA table_info(expenses)")
	if err != nil {
		db.Close()
		return nil, err
	}

	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, primaryKey int
		var defaultValue sql.NullString

		if err := rows.Scan(
			&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey,
		); err != nil {
			rows.Close()
			db.Close()
			return nil, err
		}

		if name == "expense_date" {
			dateColumnExists = true
		}
	}

	if err := rows.Err(); err != nil {
		rows.Close()
		db.Close()
		return nil, err
	}
	rows.Close()

	if !dateColumnExists {
		if _, err := db.Exec(
			"ALTER TABLE expenses ADD COLUMN expense_date TEXT NOT NULL DEFAULT ''",
		); err != nil {
			db.Close()
			return nil, err
		}
	}

	if _, err := db.Exec(query); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

func importOldExpenses(db *sql.DB, filename string) error {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM expenses").Scan(&count)
	if err != nil {
		return err
	}

	// Import the old text file only when the database is empty.
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

		name := strings.TrimSpace(parts[0])
		amount, err := strconv.ParseFloat(parts[1], 64)
		if err != nil || name == "" || amount <= 0 {
			continue
		}

		if _, err := tx.Exec(
			"INSERT INTO expenses (category, amount) VALUES (?, ?)",
			name, amount,
		); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	fmt.Println("Old expenses imported into SQLite.")
	return nil
}
