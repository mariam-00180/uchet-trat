package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
)

var database *sql.DB

var homeTemplate = template.Must(template.New("home").Parse(`
<!DOCTYPE html>
<html lang="ru">

<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<title>Учёт личных трат</title>

	<style>
		* {
			box-sizing: border-box;
		}

		body {
			margin: 0;
			padding: 30px 16px;
			font-family: Arial, sans-serif;
			background: #fff5f8;
			color: #513b46;
		}

		.container {
			max-width: 950px;
			margin: 0 auto;
		}

		header {
			text-align: center;
			margin-bottom: 30px;
		}

		header h1 {
			color: #c75d86;
			margin-bottom: 8px;
		}

		header p {
			color: #927581;
		}

		.cards {
			display: grid;
			grid-template-columns: repeat(2, minmax(0, 1fr));
			gap: 18px;
			margin-bottom: 28px;
		}

		.card {
			background: #ffffff;
			padding: 24px;
			border: 1px solid #f7dce7;
			border-radius: 18px;
			box-shadow: 0 5px 18px rgba(190, 105, 139, 0.08);
		}

		.card h3 {
			margin: 0 0 12px;
			color: #927581;
			font-size: 15px;
		}

		.card .value {
			font-size: 28px;
			font-weight: bold;
			color: #c75d86;
		}

		.section {
			background: #ffffff;
			padding: 24px;
			margin-bottom: 24px;
			border: 1px solid #f7dce7;
			border-radius: 18px;
			box-shadow: 0 5px 18px rgba(190, 105, 139, 0.06);
		}

		.section h2 {
			margin-top: 0;
			color: #c75d86;
		}
			input {
    width: 100%;
    padding: 12px;
    margin-top: 8px;
    border: 1px solid #f0c6d7;
    border-radius: 10px;
    font-size: 16px;
    outline: none;
}

input:focus {
    border-color: #c75d86;
}

button {
    background: #c75d86;
    color: white;
    border: none;
    padding: 12px 22px;
    border-radius: 10px;
    font-size: 16px;
    cursor: pointer;
}

button:hover {
    background: #ad456f;
}

		.table-wrap {
			overflow-x: auto;
		}

		table {
			width: 100%;
			border-collapse: collapse;
		}

		th, td {
			padding: 14px;
			text-align: left;
			border-bottom: 1px solid #f8e5ec;
		}

		th {
			background: #fff0f5;
			color: #a84e73;
		}

		tbody tr:hover {
			background: #fff8fa;
		}

		@media (max-width: 600px) {
			.cards {
				grid-template-columns: 1fr;
			}

			.section {
				padding: 16px;
			}
		}
	</style>
</head>


<body>
	<div class="container">
		<header>
			<h1>Учёт личных трат</h1>
			<p>Ваши расходы — под контролем</p>
		</header>

		<div class="cards">
			<div class="card">
				<h3>Общая сумма расходов</h3>
				<div class="value">{{printf "%.2f" .Total}} руб.</div>
			</div>

			<div class="card">
				<h3>Количество расходов</h3>
				<div class="value">{{.Count}}</div>
			</div>
		</div>

		<div class="section">
			<h2>Список расходов</h2>

	{{if .Expenses}}
	<div class="table-wrap">
	
<table>
    <tr>
        <th>ID</th>
        <th>Категория</th>
        <th>Сумма</th>
        <th>Дата</th>
        <th>Действия</th>
    </tr>
		{{range .Expenses}}
		<tr>
    <td>{{.ID}}</td>
    <td>{{.Category}}</td>
    <td>{{printf "%.2f" .Amount}} руб.</td>
	<td>{{if eq .ExpenseDate "1970-01-01"}}—{{else if .ExpenseDate}}{{.ExpenseDate}}{{else}}—{{end}}</td>
    <td>
       <a href="/edit?id={{.ID}}" class="edit-button">Изменить</a>

<form action="/delete" method="POST"
      onsubmit="return confirm('Вы уверены, что хотите удалить расход?');">
    <input type="hidden" name="id" value="{{.ID}}">
    <button type="submit" class="delete-button">Удалить</button>
</form>
    </td>
</tr>
		{{end}}
	</table>
	</div>
	{{else}}
	<p>Расходов пока нет.</p>
	{{end}}


<h2>Фильтр расходов</h2>

<form action="/" method="GET">
    <label>Категория:</label>
    <input type="text" name="category" placeholder="Например, еда">

    <br><br>

    <label>Дата с:</label>
    <input type="date" name="date_from">

    <br><br>

    <label>Дата по:</label>
    <input type="date" name="date_to">

    <br><br>

    <button type="submit">Фильтровать</button>
    <a href="/">Сбросить</a>
</form>

<h2>Добавить расход</h2>

<form action="/add" method="POST">
	<label>Категория:</label>
	<input type="text" name="category" required>

	<br><br>

	<label>Сумма (руб.):</label>
	<input type="number" name="amount" step="0.01" min="0.01" required>

	<br><br>

	<button type="submit">Добавить расход</button>
</form>

	</div>
	</div>
</body>
</html>

`))
var editTemplate = template.Must(template.New("edit").Parse(`
<!DOCTYPE html>
<html lang="ru">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<title>Изменить расход</title>
	<style>
		body {
			margin: 0;
			padding: 30px 16px;
			font-family: Arial, sans-serif;
			background: #fff5f8;
			color: #513b46;
		}
		.container {
			max-width: 600px;
			margin: 40px auto;
			background: white;
			padding: 28px;
			border: 1px solid #f7dce7;
			border-radius: 18px;
		}
		h1 {
			color: #c75d86;
		}
		label {
			display: block;
			margin-top: 18px;
		}
		input {
			width: 100%;
			box-sizing: border-box;
			padding: 12px;
			margin-top: 8px;
			border: 1px solid #f0c6d7;
			border-radius: 10px;
			font-size: 16px;
		}
		button {
			margin-top: 22px;
			background: #c75d86;
			color: white;
			border: none;
			padding: 12px 22px;
			border-radius: 10px;
			font-size: 16px;
			cursor: pointer;
		}
		a {
			color: #c75d86;
			margin-left: 12px;
		}
	</style>
</head>
<body>
	<div class="container">
		<h1>Изменить расход</h1>

		<form action="/edit" method="POST">
			<input type="hidden" name="id" value="{{.ID}}">

			<label>Категория:</label>
			<input type="text" name="category"
				value="{{.Category}}" required>

			<label>Сумма (руб.):</label>
			<input type="number" name="amount"
				value="{{printf "%.2f" .Amount}}"
				step="0.01" min="0.01" required>

			<button type="submit">Сохранить изменения</button>
			<a href="/">Назад</a>
		</form>
	</div>
</body>
</html>
`))

type Expense struct {
	ID          int
	Category    string
	Amount      float64
	ExpenseDate string
}

type PageData struct {
	Expenses []Expense
	Total    float64
	Count    int
}

func deleteExpenseHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	id, err := strconv.Atoi(r.FormValue("id"))
	if err != nil || id <= 0 {
		http.Error(w, "Неверный ID расхода", http.StatusBadRequest)
		return
	}

	result, err := database.Exec(
		"DELETE FROM expenses WHERE id = ?",
		id,
	)
	if err != nil {
		http.Error(w, "Ошибка удаления расхода", http.StatusInternalServerError)
		log.Println(err)
		return
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		http.Error(w, "Ошибка проверки удаления", http.StatusInternalServerError)
		log.Println(err)
		return
	}

	if rowsAffected == 0 {
		http.Error(w, "Расход не найден", http.StatusNotFound)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func editExpenseHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		id, err := strconv.Atoi(r.URL.Query().Get("id"))
		if err != nil || id <= 0 {
			http.Error(w, "Неверный ID расхода", http.StatusBadRequest)
			return
		}

		var expense Expense
		err = database.QueryRow(
			"SELECT id, category, amount FROM expenses WHERE id = ?",
			id,
		).Scan(&expense.ID, &expense.Category, &expense.Amount)

		if err == sql.ErrNoRows {
			http.Error(w, "Расход не найден", http.StatusNotFound)
			return
		}
		if err != nil {
			log.Println(err)
			http.Error(w, "Ошибка чтения расхода", http.StatusInternalServerError)
			return
		}

		if err := editTemplate.Execute(w, expense); err != nil {
			log.Println(err)
		}
		return
	}

	if r.Method == http.MethodPost {
		id, err := strconv.Atoi(r.FormValue("id"))
		category := strings.TrimSpace(r.FormValue("category"))
		amount, amountErr := strconv.ParseFloat(
			strings.TrimSpace(r.FormValue("amount")), 64,
		)

		if err != nil || id <= 0 || category == "" ||
			amountErr != nil || amount <= 0 {
			http.Error(w, "Введите правильную категорию и сумму", http.StatusBadRequest)
			return
		}

		result, err := database.Exec(
			"UPDATE expenses SET category = ?, amount = ? WHERE id = ?",
			category, amount, id,
		)
		if err != nil {
			log.Println(err)
			http.Error(w, "Ошибка изменения расхода", http.StatusInternalServerError)
			return
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			log.Println(err)
			http.Error(w, "Ошибка проверки изменения", http.StatusInternalServerError)
			return
		}
		if rowsAffected == 0 {
			http.Error(w, "Расход не найден", http.StatusNotFound)
			return
		}

		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
}

func addExpenseHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	category := strings.TrimSpace(r.FormValue("category"))
	amountText := strings.TrimSpace(r.FormValue("amount"))

	amount, err := strconv.ParseFloat(amountText, 64)
	if category == "" || err != nil || amount <= 0 {
		http.Error(w, "Введите правильную категорию и сумму", http.StatusBadRequest)
		return
	}
	_, err = database.Exec(
		"INSERT INTO expenses (category, amount, expense_date) VALUES (?, ?, date('now'))",
		category, amount,
	)

	if err != nil {
		http.Error(w, "Ошибка сохранения расхода", http.StatusInternalServerError)
		log.Println(err)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	dateFrom := r.URL.Query().Get("date_from")
	dateTo := r.URL.Query().Get("date_to")
	var rows *sql.Rows
	var err error

	if category != "" {
		rows, err = database.Query(
			"SELECT id, category, amount, expense_date FROM expenses WHERE category LIKE ? ORDER BY id",
			"%"+category+"%",
		)
	} else {
		rows, err = database.Query(
			"SELECT id, category, amount, expense_date FROM expenses ORDER BY id",
		)
	}

	query := `
    SELECT id, category, amount, expense_date
    FROM expenses
    WHERE (category LIKE ? OR ? = '')
      AND (expense_date >= ? OR ? = '')
      AND (expense_date <= ? OR ? = '')
    ORDER BY id
`

	rows, err = database.Query(
		query,
		"%"+category+"%", category,
		dateFrom, dateFrom,
		dateTo, dateTo,
	)

	if err != nil {
		http.Error(w, "Ошибка чтения расходов", http.StatusInternalServerError)
		log.Println(err)
		return
	}
	defer rows.Close()

	data := PageData{
		Expenses: []Expense{},
	}

	for rows.Next() {
		var expense Expense
		if err := rows.Scan(
			&expense.ID,
			&expense.Category,
			&expense.Amount,
			&expense.ExpenseDate,
		); err != nil {

			http.Error(w, "Ошибка чтения данных", http.StatusInternalServerError)
			log.Println(err)
			return
		}

		data.Expenses = append(data.Expenses, expense)
		data.Total += expense.Amount
		data.Count++
	}

	if err := rows.Err(); err != nil {
		http.Error(w, "Ошибка чтения расходов", http.StatusInternalServerError)
		log.Println(err)
		return
	}

	if err := homeTemplate.Execute(w, data); err != nil {
		log.Println(err)
	}
}

func startWebServer(db *sql.DB) {
	database = db

	mux := http.NewServeMux()
	mux.HandleFunc("/", homeHandler)
	mux.HandleFunc("/add", addExpenseHandler)
	mux.HandleFunc("/delete", deleteExpenseHandler)
	mux.HandleFunc("/edit", editExpenseHandler)
	fmt.Println("Сервер запущен: http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
