package main

import (
	"bufio"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

func main() {
	db, err := openDatabase()
	if err != nil {
		fmt.Println("Ошибка открытия базы данных:", err)
		return
	}
	defer db.Close()

	if err := importOldExpenses(db, "expenses.txt"); err != nil {
		fmt.Println("Ошибка импорта старых расходов:", err)
		return
	}

	startWebServer(db)
}

func addExpense(db *sql.DB, reader *bufio.Reader) {
	fmt.Println("Добавление расхода")
	fmt.Println("Для завершения введите: стоп")

	for {
		fmt.Print("\nВведите название расхода: ")
		name, err := reader.ReadString('\n')
		if err != nil && len(name) == 0 {
			return
		}
		name = strings.TrimSpace(name)

		if strings.EqualFold(name, "стоп") {
			break
		}
		if name == "" || strings.ContainsAny(name, "\t\n\r") {
			fmt.Println("Введите название расхода.")
			continue
		}

		fmt.Print("Введите сумму расхода: ")
		amountText, err := reader.ReadString('\n')
		if err != nil && len(amountText) == 0 {
			return
		}
		amountText = strings.TrimSpace(amountText)
		amount, err := strconv.ParseFloat(
			strings.ReplaceAll(amountText, ",", "."),
			64,
		)

		if err != nil || amount <= 0 {
			fmt.Println("Введите сумму больше нуля.")
			continue
		}

		_, err = db.Exec(
			"INSERT INTO expenses (category, amount) VALUES (?, ?)",
			name, amount,
		)
		if err != nil {
			fmt.Println("Ошибка сохранения:", err)
			continue
		}

		fmt.Println("Расход сохранён!")
	}

	showTotal(db)
}

func showExpenses(db *sql.DB) {
	fmt.Println("\n--- Список расходов ---")

	rows, err := db.Query(
		"SELECT id, category, amount FROM expenses ORDER BY id",
	)
	if err != nil {
		fmt.Println("Ошибка чтения:", err)
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var id int
		var category string
		var amount float64

		if err := rows.Scan(&id, &category, &amount); err != nil {
			fmt.Println("Ошибка чтения записи:", err)
			return
		}

		count++
		fmt.Printf("%d. %s — %.2f руб.\n", id, category, amount)
	}

	if err := rows.Err(); err != nil {
		fmt.Println("Ошибка чтения:", err)
		return
	}
	if count == 0 {
		fmt.Println("Расходов пока нет.")
	}
}

func showTotal(db *sql.DB) {
	var count int
	var total float64

	err := db.QueryRow(
		"SELECT COUNT(*), COALESCE(SUM(amount), 0) FROM expenses",
	).Scan(&count, &total)
	if err != nil {
		fmt.Println("Ошибка подсчёта:", err)
		return
	}

	fmt.Println("Количество расходов:", count)
	fmt.Printf("Общая сумма: %.2f руб.\n", total)
}

func deleteExpense(db *sql.DB, reader *bufio.Reader) {
	showExpenses(db)

	fmt.Print("Введите ID расхода для удаления: ")
	text, err := reader.ReadString('\n')
	if err != nil && len(text) == 0 {
		return
	}

	id, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || id <= 0 {
		fmt.Println("Неверный номер.")
		return
	}

	result, err := db.Exec("DELETE FROM expenses WHERE id = ?", id)
	if err != nil {
		fmt.Println("Ошибка удаления:", err)
		return
	}

	affected, err := result.RowsAffected()
	if err != nil {
		fmt.Println("Ошибка проверки удаления:", err)
		return
	}
	if affected == 0 {
		fmt.Println("Расход с таким ID не найден.")
		return
	}

	fmt.Println("Расход удалён!")
	showTotal(db)
}

func showMenu() {
	fmt.Println("\n--- Главное меню ---")
	fmt.Println("1. Добавить расход")
	fmt.Println("2. Показать расходы")
	fmt.Println("3. Показать общую сумму")
	fmt.Println("4. Выход")
	fmt.Println("5. Удалить расход")
	fmt.Print("Выберите действие: ")
}
