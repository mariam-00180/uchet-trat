package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type App struct {
	db   *sql.DB
	repo *ExpenseRepository
}

//go:embed templates/404.html templates/500.html
var errorTemplateFiles embed.FS

var errorTemplates = template.Must(template.ParseFS(errorTemplateFiles, "templates/404.html", "templates/500.html"))

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}
func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func writeErrorPage(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	name := "500.html"
	if status == http.StatusNotFound {
		name = "404.html"
	}
	if err := errorTemplates.ExecuteTemplate(w, name, nil); err != nil {
		http.Error(w, http.StatusText(status), status)
	}
}

type User struct {
	ID       int
	Username string
}
type Expense struct {
	ID          int
	Category    string
	Amount      float64
	ExpenseDate string
}
type CategoryStat struct {
	Category string
	Total    float64
	Percent  int
}
type MonthStat struct {
	Month   string
	Total   float64
	Percent int
}
type PageData struct {
	User         User
	Expenses     []Expense
	Total        float64
	Count        int
	Category     string
	DateFrom     string
	DateTo       string
	Categories   []CategoryStat
	Months       []MonthStat
	Message      string
	Error        string
	FormCategory string
	FormAmount   string
	FormDate     string
	CSRF         string
	Username     string
}

var pageTemplate = template.Must(template.New("page").Funcs(template.FuncMap{
	"money": func(n float64) string { return fmt.Sprintf("%.2f", n) },
}).Parse(`<!doctype html><html lang="ru"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.Title}}</title><link rel="stylesheet" href="/static/style.css"></head><body><main class="container">
{{if .Auth}}<header><div><h1>Учёт личных трат</h1><p>Ваши расходы — под контролем</p></div><nav class="top-links"><span>Пользователь: <b>{{.User.Username}}</b></span><a class="button secondary" href="/stats">Статистика</a><a class="button secondary" href="/export.csv">Экспорт CSV</a><form action="/logout" method="post"><input type="hidden" name="csrf" value="{{.CSRF}}"><button type="submit">Выйти</button></form></nav></header>{{end}}
{{if .Message}}<div class="notice success">{{.Message}}</div>{{end}}{{if .Error}}<div class="notice error">{{.Error}}</div>{{end}}
{{if eq .Kind "login"}}<section class="auth-card"><h1>Вход</h1><p class="muted">Войдите, чтобы открыть свои расходы.</p><form method="post" action="/login"><input type="hidden" name="csrf" value="{{.CSRF}}"><label>Имя пользователя</label><input name="username" autocomplete="username" required maxlength="40" value="{{.Username}}"><label>Пароль</label><input type="password" name="password" autocomplete="current-password" required><button type="submit">Войти</button></form><p>Нет аккаунта? <a href="/register">Зарегистрироваться</a></p></section>
{{else if eq .Kind "register"}}<section class="auth-card"><h1>Регистрация</h1><p class="muted">Имя — 3–40 символов, пароль — не менее 8 символов.</p><form method="post" action="/register"><input type="hidden" name="csrf" value="{{.CSRF}}"><label>Имя пользователя</label><input name="username" autocomplete="username" required minlength="3" maxlength="40" value="{{.Username}}"><label>Пароль</label><input type="password" name="password" autocomplete="new-password" required minlength="8"><label>Повторите пароль</label><input type="password" name="confirm" autocomplete="new-password" required minlength="8"><button type="submit">Создать аккаунт</button></form><p>Уже есть аккаунт? <a href="/login">Войти</a></p></section>
{{else if eq .Kind "home"}}<section class="cards"><div class="card"><h3>Сумма расходов по фильтру</h3><div class="value">{{money .Total}} руб.</div></div><div class="card"><h3>Количество расходов</h3><div class="value">{{.Count}}</div></div></section>
<section class="section"><h2>Фильтр расходов</h2><form method="get" action="/" class="inline-form"><div><label>Категория</label><select name="category"><option value="">Все категории</option>{{range .CategoryOptions}}<option value="{{.}}" {{if eq $.Category .}}selected{{end}}>{{.}}</option>{{end}}</select></div><div><label>Дата с</label><input type="date" name="date_from" value="{{.DateFrom}}"></div><div><label>Дата по</label><input type="date" name="date_to" value="{{.DateTo}}"></div><button type="submit">Фильтровать</button></form><p><a href="/">Сбросить фильтр</a></p></section>
<section class="section"><h2>Добавить расход</h2><form action="/add" method="post"><input type="hidden" name="csrf" value="{{.CSRF}}"><label>Категория</label><select name="category" required>{{range .CategoryOptions}}<option value="{{.}}" {{if eq $.FormCategory .}}selected{{end}}>{{.}}</option>{{end}}</select><label>Сумма (руб.)</label><input type="number" name="amount" min="0.01" max="100000000" step="0.01" required value="{{.FormAmount}}"><label>Дата</label><input type="date" name="expense_date" value="{{if .FormDate}}{{.FormDate}}{{else}}{{.Today}}{{end}}" required><button type="submit">Добавить расход</button></form></section>
<section class="section"><h2>Список расходов</h2>{{if .Expenses}}<div class="table-wrap"><table><thead><tr><th>ID</th><th>Категория</th><th>Сумма</th><th>Дата</th><th>Действия</th></tr></thead><tbody>{{range .Expenses}}<tr><td>{{.ID}}</td><td>{{.Category}}</td><td>{{money .Amount}} руб.</td><td>{{if or (eq .ExpenseDate "1970-01-01") (eq .ExpenseDate "")}}—{{else}}{{.ExpenseDate}}{{end}}</td><td><div class="actions"><a class="button secondary" href="/edit?id={{.ID}}">Изменить</a><form action="/delete" method="post" onsubmit="return confirm('Удалить расход?')"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="id" value="{{.ID}}"><button class="danger" type="submit">Удалить</button></form></div></td></tr>{{end}}</tbody></table></div>{{else}}<p class="muted">Расходов по выбранному фильтру нет.</p>{{end}}</section>
{{else if eq .Kind "edit"}}<section class="section"><h2>Изменить расход</h2><form action="/edit" method="post"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="id" value="{{.Edit.ID}}"><label>Категория</label><select name="category" required>{{range .CategoryOptions}}<option value="{{.}}" {{if eq $.Edit.Category .}}selected{{end}}>{{.}}</option>{{end}}</select><label>Сумма (руб.)</label><input type="number" name="amount" min="0.01" max="100000000" step="0.01" required value="{{if .FormAmount}}{{.FormAmount}}{{else}}{{money .Edit.Amount}}{{end}}"><label>Дата</label><input type="date" name="expense_date" required value="{{if .FormDate}}{{.FormDate}}{{else}}{{.Edit.ExpenseDate}}{{end}}"><button type="submit">Сохранить изменения</button> <a href="/">Назад</a></form></section>
{{else if eq .Kind "stats"}}<section class="cards"><div class="card"><h3>Всего потрачено</h3><div class="value">{{money .Total}} руб.</div></div><div class="card"><h3>Количество расходов</h3><div class="value">{{.Count}}</div></div></section><section class="section"><h2>Расходы по категориям</h2>{{if .Categories}}{{range .Categories}}<div class="bar-row"><span>{{.Category}}</span><div class="bar"><span style="width:{{.Percent}}%"></span></div><b>{{money .Total}}</b></div>{{end}}{{else}}<p>Пока нет данных для статистики.</p>{{end}}</section><section class="section"><h2>Расходы по месяцам</h2>{{if .Months}}{{range .Months}}<div class="bar-row"><span>{{.Month}}</span><div class="bar"><span style="width:{{.Percent}}%"></span></div><b>{{money .Total}}</b></div>{{end}}{{else}}<p>Пока нет данных для статистики.</p>{{end}}</section><section class="section"><h2>Графики статистики</h2><div class="chart-grid"><div><canvas id="categoryChart" aria-label="Расходы по категориям"></canvas></div><div><canvas id="monthChart" aria-label="Расходы по месяцам"></canvas></div></div></section><script src="https://cdn.jsdelivr.net/npm/chart.js"></script><script>
(async function(){
 if(!window.Chart)return;
 try {
  const [catRes,monthRes]=await Promise.all([fetch('/api/stats/by-category'),fetch('/api/stats/by-month')]);
  if(!catRes.ok||!monthRes.ok)throw new Error('Не удалось загрузить статистику');
  const cats=await catRes.json(), months=await monthRes.json();
  new Chart(document.getElementById('categoryChart'),{type:'pie',data:{labels:cats.map(x=>x.label),datasets:[{data:cats.map(x=>x.value)}]},options:{responsive:true,plugins:{title:{display:true,text:'Расходы по категориям'}}}});
  new Chart(document.getElementById('monthChart'),{type:'bar',data:{labels:months.map(x=>x.label),datasets:[{label:'Сумма расходов',data:months.map(x=>x.value)}]},options:{responsive:true,plugins:{title:{display:true,text:'Расходы по месяцам'}}}});
 } catch(e){console.error(e);}
})();</script><a class="button secondary" href="/">Назад к расходам</a>
{{end}}</main></body></html>`))

type templateData struct {
	PageData
	Kind, Title     string
	Auth            bool
	Today           string
	Edit            Expense
	CategoryOptions []string
}

func newApp(db *sql.DB) *App { return &App{db: db, repo: NewExpenseRepository(db)} }
func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.HandleFunc("/login", a.loginHandler)
	mux.HandleFunc("/register", a.registerHandler)
	mux.HandleFunc("/logout", a.logoutHandler)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			writeErrorPage(w, http.StatusNotFound)
			return
		}
		a.authRequired(a.homeHandler)(w, r)
	})
	mux.HandleFunc("/add", a.authRequired(a.addHandler))
	mux.HandleFunc("/edit", a.authRequired(a.editHandler))
	mux.HandleFunc("/delete", a.authRequired(a.deleteHandler))
	mux.HandleFunc("/stats", a.authRequired(a.statsHandler))
	mux.HandleFunc("/export.csv", a.authRequired(a.exportHandler))
	mux.HandleFunc("/api/stats/by-category", a.authRequired(a.statsByCategoryAPI))
	mux.HandleFunc("/api/stats/by-month", a.authRequired(a.statsByMonthAPI))
	return requestLogger(recoveryMiddleware(a.csrfCookie(mux)))
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		status := sw.status
		if status == 0 {
			status = http.StatusOK
		}
		log.Printf("%s %s status=%d duration=%s", r.Method, r.URL.Path, status, time.Since(start).Round(time.Millisecond))
	})
}

func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				log.Printf("panic recovered: %v", v)
				// A handler may already have written headers; still keep the server alive.
				writeErrorPage(w, http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (a *App) csrfCookie(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("csrf_token"); err != nil || c.Value == "" {
			token := randomToken()
			http.SetCookie(w, &http.Cookie{Name: "csrf_token", Value: token, Path: "/", SameSite: http.SameSiteLaxMode, Secure: requestIsHTTPS(r), HttpOnly: false, MaxAge: 86400})
			r.AddCookie(&http.Cookie{Name: "csrf_token", Value: token, Path: "/"})
		}
		next.ServeHTTP(w, r)
	})
}
func requestIsHTTPS(r *http.Request) bool { return r.TLS != nil }
func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func hashToken(s string) string {
	secret := strings.TrimSpace(os.Getenv("SESSION_SECRET"))
	if secret == "" {
		sum := sha256.Sum256([]byte(s))
		return fmt.Sprintf("%x", sum[:])
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(s))
	return fmt.Sprintf("%x", mac.Sum(nil))
}
func csrfValid(r *http.Request) bool {
	c, err := r.Cookie("csrf_token")
	return err == nil && c.Value != "" && hmac.Equal([]byte(c.Value), []byte(r.FormValue("csrf")))
}
func (a *App) render(
	w http.ResponseWriter,
	r *http.Request,
	kind, title string,
	data PageData,
	edit Expense,
) {
	csrf := ""
	if c, err := r.Cookie("csrf_token"); err == nil {
		csrf = c.Value
	}

	user, ok := a.currentUser(r)

	d := templateData{
		PageData: data,
		Kind:     kind,
		Title:    title,
		Auth:     ok,
		Today:    time.Now().Format("2006-01-02"),
		Edit:     edit,
	}

	categories, err := a.repo.CategoryNames()
	if err != nil {
		log.Printf("load categories: %v", err)
		writeErrorPage(w, http.StatusInternalServerError)
		return
	}

	d.CategoryOptions = categories
	d.CSRF = csrf
	d.User = user

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if err := pageTemplate.Execute(w, d); err != nil {
		log.Printf("template error: %v", err)
	}
}
func (a *App) authRequired(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := a.currentUser(r); !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}
func (a *App) currentUser(r *http.Request) (User, bool) {
	c, err := r.Cookie("session")
	if err != nil || c.Value == "" {
		return User{}, false
	}
	u, err := a.repo.UserForSession(hashToken(c.Value))
	return u, err == nil
}
func (a *App) createSession(w http.ResponseWriter, r *http.Request, userID int) error {
	token := randomToken()
	expires := time.Now().Add(7 * 24 * time.Hour).UTC().Format("2006-01-02 15:04:05")
	if err := a.repo.CreateSession(hashToken(token), userID, expires); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: "session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: requestIsHTTPS(r), Expires: time.Now().Add(7 * 24 * time.Hour)})
	return nil
}
func validPost(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return false
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Некорректная форма", http.StatusBadRequest)
		return false
	}
	if !csrfValid(r) {
		http.Error(w, "Срок действия формы истёк. Обновите страницу и повторите попытку.", http.StatusForbidden)
		return false
	}
	return true
}

func (a *App) loginHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentUser(r); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if r.Method == http.MethodGet {
		a.render(w, r, "login", "Вход", PageData{}, Expense{})
		return
	}
	if !validPost(w, r) {
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	id, stored, err := a.repo.Authenticate(username)
	if err != nil || !verifyPassword(stored, password) {
		a.render(w, r, "login", "Вход", PageData{Error: "Неверное имя пользователя или пароль.", Username: username}, Expense{})
		return
	}
	if strings.HasPrefix(stored, "pbkdf2$") {
		if upgraded, e := hashPassword(password); e == nil {
			if e = a.repo.UpdatePasswordHash(id, upgraded); e != nil {
				log.Println("password hash upgrade:", e)
			}
		}
	}
	if err := a.createSession(w, r, id); err != nil {
		log.Println(err)
		writeErrorPage(w, 500)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
func (a *App) registerHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentUser(r); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if r.Method == http.MethodGet {
		a.render(w, r, "register", "Регистрация", PageData{}, Expense{})
		return
	}
	if !validPost(w, r) {
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	if len(username) < 3 || len(username) > 40 || strings.ContainsAny(username, " \t\r\n") {
		a.render(w, r, "register", "Регистрация", PageData{Error: "Имя пользователя должно содержать 3–40 символов без пробелов.", Username: username}, Expense{})
		return
	}
	if len(password) < 8 || len(password) > 200 {
		a.render(w, r, "register", "Регистрация", PageData{Error: "Пароль должен содержать не менее 8 символов.", Username: username}, Expense{})
		return
	}
	if password != r.FormValue("confirm") {
		a.render(w, r, "register", "Регистрация", PageData{Error: "Пароли не совпадают.", Username: username}, Expense{})
		return
	}
	encoded, err := hashPassword(password)
	if err != nil {
		writeErrorPage(w, 500)
		return
	}
	id, err := a.repo.Register(username, encoded)
	if err != nil {
		a.render(w, r, "register", "Регистрация", PageData{Error: "Не удалось создать аккаунт. Возможно, такое имя уже занято.", Username: username}, Expense{})
		return
	}
	if err := a.createSession(w, r, id); err != nil {
		log.Println(err)
		writeErrorPage(w, 500)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
func (a *App) logoutHandler(w http.ResponseWriter, r *http.Request) {
	if !validPost(w, r) {
		return
	}
	if c, err := r.Cookie("session"); err == nil {
		if err := a.repo.DeleteSession(hashToken(c.Value)); err != nil {
			log.Println("delete session:", err)
		}
	}
	http.SetCookie(w, &http.Cookie{Name: "session", Value: "", Path: "/", HttpOnly: true, MaxAge: -1, Expires: time.Unix(0, 0), SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
func (a *App) homeHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeErrorPage(w, 404)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Метод не поддерживается", 405)
		return
	}
	user, _ := a.currentUser(r)
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	from := r.URL.Query().Get("date_from")
	to := r.URL.Query().Get("date_to")
	if !validDateOrEmpty(from) || !validDateOrEmpty(to) || (from != "" && to != "" && from > to) {
		data := PageData{Category: category, DateFrom: from, DateTo: to, Error: "Укажите корректный диапазон дат."}
		expenses, err := a.repo.GetAll(user.ID, "", "", "")
		if err != nil {
			log.Println("load expenses after filter validation error:", err)
			writeErrorPage(w, http.StatusInternalServerError)
			return
		}
		data.Expenses = expenses
		for _, e := range expenses {
			data.Total += e.Amount
			data.Count++
		}
		w.WriteHeader(http.StatusBadRequest)
		a.render(w, r, "home", "Учёт личных трат", data, Expense{})
		return
	}
	expenses, err := a.repo.GetAll(user.ID, category, from, to)
	if err != nil {
		log.Println(err)
		writeErrorPage(w, 500)
		return
	}
	data := PageData{Category: category, DateFrom: from, DateTo: to, Expenses: expenses}
	for _, e := range expenses {
		data.Total += e.Amount
		data.Count++
	}
	a.render(w, r, "home", "Учёт личных трат", data, Expense{})
}
func (a *App) addHandler(w http.ResponseWriter, r *http.Request) {
	if !validPost(w, r) {
		return
	}
	user, _ := a.currentUser(r)
	category := strings.TrimSpace(r.FormValue("category"))
	amount, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(r.FormValue("amount")), ",", "."), 64)
	date := r.FormValue("expense_date")
	if category == "" || len(category) > 80 || err != nil || math.IsNaN(amount) || math.IsInf(amount, 0) || amount <= 0 || amount > 100000000 || !validDate(date) {
		data := PageData{Error: "Введите категорию, сумму больше нуля и корректную дату.", FormCategory: category, FormAmount: strings.TrimSpace(r.FormValue("amount")), FormDate: date}
		expenses, loadErr := a.repo.GetAll(user.ID, "", "", "")
		if loadErr != nil {
			log.Println("load expenses after validation error:", loadErr)
			writeErrorPage(w, http.StatusInternalServerError)
			return
		}
		data.Expenses = expenses
		for _, e := range expenses {
			data.Total += e.Amount
			data.Count++
		}

		a.render(w, r, "home", "Учёт личных трат", data, Expense{})
		return
	}
	if err := a.repo.Create(category, amount, date, user.ID); err != nil {
		log.Println(err)
		writeErrorPage(w, 500)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
func (a *App) editHandler(w http.ResponseWriter, r *http.Request) {
	user, _ := a.currentUser(r)
	if r.Method == http.MethodGet {
		id, err := strconv.Atoi(r.URL.Query().Get("id"))
		if err != nil || id <= 0 {
			http.Error(w, "Неверный ID", 400)
			return
		}
		e, err := a.repo.GetByID(id, user.ID)
		if err == sql.ErrNoRows {
			writeErrorPage(w, 404)
			return
		}
		if err != nil {
			log.Println(err)
			writeErrorPage(w, 500)
			return
		}
		a.render(w, r, "edit", "Изменить расход", PageData{}, e)
		return
	}
	if !validPost(w, r) {
		return
	}
	id, err := strconv.Atoi(r.FormValue("id"))
	category := strings.TrimSpace(r.FormValue("category"))
	amount, amountErr := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(r.FormValue("amount")), ",", "."), 64)
	date := r.FormValue("expense_date")
	if err != nil || id <= 0 || category == "" || len(category) > 80 || amountErr != nil || amount <= 0 || amount > 100000000 || math.IsNaN(amount) || math.IsInf(amount, 0) || !validDate(date) {
		edit := Expense{ID: id, Category: category, ExpenseDate: date}
		data := PageData{Error: "Введите правильные данные: категорию, положительную сумму и корректную дату.", FormAmount: strings.TrimSpace(r.FormValue("amount")), FormDate: date}
		w.WriteHeader(http.StatusBadRequest)
		a.render(w, r, "edit", "Изменить расход", data, edit)
		return
	}
	found, err := a.repo.Update(id, user.ID, category, amount, date)
	if err != nil {
		log.Println(err)
		writeErrorPage(w, 500)
		return
	}
	if !found {
		writeErrorPage(w, 404)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
func (a *App) deleteHandler(w http.ResponseWriter, r *http.Request) {
	if !validPost(w, r) {
		return
	}
	user, _ := a.currentUser(r)
	id, err := strconv.Atoi(r.FormValue("id"))
	if err != nil || id <= 0 {
		http.Error(w, "Неверный ID", 400)
		return
	}
	found, err := a.repo.Delete(id, user.ID)
	if err != nil {
		log.Println(err)
		writeErrorPage(w, 500)
		return
	}
	if !found {
		writeErrorPage(w, 404)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
func (a *App) statsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Метод не поддерживается", 405)
		return
	}
	user, _ := a.currentUser(r)
	data := PageData{}
	count, total, err := a.repo.Summary(user.ID)
	if err != nil {
		log.Println(err)
		writeErrorPage(w, 500)
		return
	}
	data.Count = count
	data.Total = total
	data.Categories, err = a.repo.CategoryStats(user.ID)
	if err != nil {
		log.Println(err)
		writeErrorPage(w, 500)
		return
	}
	data.Months, err = a.repo.MonthStats(user.ID, 12)
	if err != nil {
		log.Println(err)
		writeErrorPage(w, 500)
		return
	}
	var maxCat, maxMonth float64
	for _, v := range data.Categories {
		if v.Total > maxCat {
			maxCat = v.Total
		}
	}
	for i := range data.Categories {
		if maxCat > 0 {
			data.Categories[i].Percent = int(math.Round(data.Categories[i].Total / maxCat * 100))
		}
	}
	for _, v := range data.Months {
		if v.Total > maxMonth {
			maxMonth = v.Total
		}
	}
	for i := range data.Months {
		if maxMonth > 0 {
			data.Months[i].Percent = int(math.Round(data.Months[i].Total / maxMonth * 100))
		}
	}
	a.render(w, r, "stats", "Статистика расходов", data, Expense{})
}
func (a *App) statsByCategoryAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}
	user, ok := a.currentUser(r)
	if !ok {
		http.Error(w, "Требуется вход", http.StatusUnauthorized)
		return
	}
	out, err := a.repo.CategoryPoints(user.ID)
	if err != nil {
		log.Printf("category stats: %v", err)
		writeErrorPage(w, 500)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		log.Printf("encode category stats: %v", err)
	}
}
func (a *App) statsByMonthAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}
	user, ok := a.currentUser(r)
	if !ok {
		http.Error(w, "Требуется вход", http.StatusUnauthorized)
		return
	}
	out, err := a.repo.MonthPoints(user.ID)
	if err != nil {
		log.Printf("monthly stats: %v", err)
		writeErrorPage(w, 500)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		log.Printf("encode monthly stats: %v", err)
	}
}
func (a *App) exportHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Метод не поддерживается", 405)
		return
	}
	user, _ := a.currentUser(r)
	expenses, err := a.repo.Export(user.ID)
	if err != nil {
		log.Println(err)
		writeErrorPage(w, 500)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=expenses.csv")
	w.Write([]byte{0xEF, 0xBB, 0xBF})
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"Категория", "Сумма", "Дата"}); err != nil {
		log.Println(err)
		return
	}
	for _, e := range expenses {
		if err := cw.Write([]string{e.Category, fmt.Sprintf("%.2f", e.Amount), e.ExpenseDate}); err != nil {
			log.Println(err)
			return
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		log.Println(err)
	}
}
func validDateOrEmpty(s string) bool { return s == "" || validDate(s) }
func validDate(s string) bool {
	t, err := time.Parse("2006-01-02", s)
	return err == nil && t.Format("2006-01-02") == s
}

// Passwords are stored with bcrypt. Existing PBKDF2 hashes remain verifiable so
// accounts created by older versions can still log in; successful legacy logins
// are upgraded to bcrypt.
func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}
func verifyPassword(encoded, password string) bool {
	if strings.HasPrefix(encoded, "$2a$") || strings.HasPrefix(encoded, "$2b$") || strings.HasPrefix(encoded, "$2y$") {
		return bcrypt.CompareHashAndPassword([]byte(encoded), []byte(password)) == nil
	}
	// Backward compatibility with the previous PBKDF2 format.
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2" {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 10000 || iterations > 1000000 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	actual := pbkdf2SHA256([]byte(password), salt, iterations, len(expected))
	return hmac.Equal(actual, expected)
}
func pbkdf2SHA256(password, salt []byte, iterations, keyLen int) []byte {
	const hLen = 32
	blocks := (keyLen + hLen - 1) / hLen
	out := make([]byte, 0, blocks*hLen)
	for block := 1; block <= blocks; block++ {
		mac := hmac.New(sha256.New, password)
		mac.Write(salt)
		mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < iterations; i++ {
			mac = hmac.New(sha256.New, password)
			mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLen]
}
