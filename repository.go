package main

import (
	"database/sql"
	"errors"
)

// ExpenseRepository is the single data-access layer. HTTP handlers should call
// these methods instead of issuing SQL statements themselves.
type ExpenseRepository struct{ db *sql.DB }

// statPoint is the compact label/value shape used by the chart API endpoints.
type statPoint struct {
    Label string  `json:"label"`
    Value float64 `json:"value"`
}

func NewExpenseRepository(db *sql.DB) *ExpenseRepository { return &ExpenseRepository{db: db} }

func (r *ExpenseRepository) CategoryNames() ([]string, error) {
	rows, err := r.db.Query(`SELECT name FROM categories ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
func (r *ExpenseRepository) UserForSession(tokenHash string) (User, error) {
	var u User
	err := r.db.QueryRow(`SELECT u.id,u.username FROM users u JOIN sessions s ON s.user_id=u.id WHERE s.token_hash=? AND s.expires_at > datetime('now')`, tokenHash).Scan(&u.ID, &u.Username)
	return u, err
}
func (r *ExpenseRepository) CreateSession(tokenHash string, userID int, expires string) error {
	_, err := r.db.Exec(`INSERT INTO sessions(token_hash,user_id,expires_at) VALUES(?,?,?)`, tokenHash, userID, expires)
	return err
}
func (r *ExpenseRepository) Authenticate(username string) (int, string, error) {
	var id int
	var hash string
	err := r.db.QueryRow(`SELECT id,password_hash FROM users WHERE username=? COLLATE NOCASE`, username).Scan(&id, &hash)
	return id, hash, err
}
func (r *ExpenseRepository) UpdatePasswordHash(userID int, hash string) error {
	_, err := r.db.Exec(`UPDATE users SET password_hash=? WHERE id=?`, hash, userID)
	return err
}
func (r *ExpenseRepository) Register(username, hash string) (int, error) {
	res, err := r.db.Exec(`INSERT INTO users(username,password_hash) VALUES(?,?)`, username, hash)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return int(id), err
}
func (r *ExpenseRepository) DeleteSession(tokenHash string) error {
	_, err := r.db.Exec(`DELETE FROM sessions WHERE token_hash=?`, tokenHash)
	return err
}
func (r *ExpenseRepository) GetAll(userID int, category, from, to string) ([]Expense, error) {
	rows, err := r.db.Query(`SELECT e.id,COALESCE(c.name,e.category),e.amount,e.expense_date FROM expenses e LEFT JOIN categories c ON c.id=e.category_id WHERE e.user_id=? AND (?='' OR COALESCE(c.name,e.category) LIKE '%'||?||'%') AND (?='' OR e.expense_date>=?) AND (?='' OR e.expense_date<=?) ORDER BY e.expense_date DESC,e.id DESC`, userID, category, category, from, from, to, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Expense{}
	for rows.Next() {
		var e Expense
		if err := rows.Scan(&e.ID, &e.Category, &e.Amount, &e.ExpenseDate); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
func (r *ExpenseRepository) GetByID(id, userID int) (Expense, error) {
	var e Expense
	err := r.db.QueryRow(`SELECT e.id,COALESCE(c.name,e.category),e.amount,e.expense_date FROM expenses e LEFT JOIN categories c ON c.id=e.category_id WHERE e.id=? AND e.user_id=?`, id, userID).Scan(&e.ID, &e.Category, &e.Amount, &e.ExpenseDate)
	return e, err
}
func (r *ExpenseRepository) ensureCategory(name string) error {
	_, err := r.db.Exec(`INSERT OR IGNORE INTO categories(name) VALUES(?)`, name)
	return err
}
func (r *ExpenseRepository) Create(category string, amount float64, date string, userID int) error {
	if err := r.ensureCategory(category); err != nil {
		return err
	}
	_, err := r.db.Exec(`INSERT INTO expenses(category,category_id,amount,expense_date,user_id) VALUES(?,(SELECT id FROM categories WHERE name=? COLLATE NOCASE),?,?,?)`, category, category, amount, date, userID)
	return err
}
func (r *ExpenseRepository) Update(id, userID int, category string, amount float64, date string) (bool, error) {
	if err := r.ensureCategory(category); err != nil {
		return false, err
	}
	res, err := r.db.Exec(`UPDATE expenses SET category=?,category_id=(SELECT id FROM categories WHERE name=? COLLATE NOCASE),amount=?,expense_date=? WHERE id=? AND user_id=?`, category, category, amount, date, id, userID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
func (r *ExpenseRepository) Delete(id, userID int) (bool, error) {
	res, err := r.db.Exec(`DELETE FROM expenses WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
func (r *ExpenseRepository) Summary(userID int) (int, float64, error) {
	var count int
	var total float64
	err := r.db.QueryRow(`SELECT COUNT(*),COALESCE(SUM(amount),0) FROM expenses WHERE user_id=?`, userID).Scan(&count, &total)
	return count, total, err
}
func (r *ExpenseRepository) CategoryStats(userID int) ([]CategoryStat, error) {
	rows, err := r.db.Query(`SELECT COALESCE(c.name,e.category),SUM(e.amount) FROM expenses e LEFT JOIN categories c ON c.id=e.category_id WHERE e.user_id=? GROUP BY COALESCE(c.name,e.category) ORDER BY SUM(e.amount) DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CategoryStat{}
	for rows.Next() {
		var s CategoryStat
		if err := rows.Scan(&s.Category, &s.Total); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
func (r *ExpenseRepository) MonthStats(userID int, limit int) ([]MonthStat, error) {
	if limit != 12 {
		return nil, errors.New("unsupported month-stat limit")
	}
	rows, err := r.db.Query(`SELECT substr(expense_date,1,7),SUM(amount) FROM expenses WHERE user_id=? AND length(expense_date)>=7 GROUP BY substr(expense_date,1,7) ORDER BY substr(expense_date,1,7) DESC LIMIT 12`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MonthStat{}
	for rows.Next() {
		var s MonthStat
		if err := rows.Scan(&s.Month, &s.Total); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
func (r *ExpenseRepository) CategoryPoints(userID int) ([]statPoint, error) {
	rows, err := r.db.Query(`SELECT COALESCE(c.name,e.category),SUM(e.amount) FROM expenses e LEFT JOIN categories c ON c.id=e.category_id WHERE e.user_id=? GROUP BY COALESCE(c.name,e.category) ORDER BY SUM(e.amount) DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []statPoint{}
	for rows.Next() {
		var p statPoint
		if err := rows.Scan(&p.Label, &p.Value); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (r *ExpenseRepository) MonthPoints(userID int) ([]statPoint, error) {
	rows, err := r.db.Query(`SELECT substr(expense_date,1,7), SUM(amount) FROM expenses WHERE user_id=? AND length(expense_date)>=7 GROUP BY substr(expense_date,1,7) ORDER BY substr(expense_date,1,7)`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []statPoint{}
	for rows.Next() {
		var p statPoint
		if err := rows.Scan(&p.Label, &p.Value); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (r *ExpenseRepository) Export(userID int) ([]Expense, error) {
	rows, err := r.db.Query(`SELECT COALESCE(c.name,e.category),e.amount,e.expense_date FROM expenses e LEFT JOIN categories c ON c.id=e.category_id WHERE e.user_id=? ORDER BY e.expense_date,e.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Expense{}
	for rows.Next() {
		var e Expense
		if err := rows.Scan(&e.Category, &e.Amount, &e.ExpenseDate); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
