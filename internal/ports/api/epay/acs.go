package epay

import (
	"html/template"
	"net/http"
)

// Значения, которые мок-ACS возвращает в PaRes. PG различает исход не по ним,
// а по последующему confirm, поэтому содержимое произвольное.
const (
	acsPaResApproved = "mock-pares-approved"
	acsPaResDeclined = "mock-pares-declined"

	// defaultACSURL — адрес страницы проверки, когда он не задан конфигурацией.
	// Пустой action означал бы, что PG некуда отправлять пользователя.
	defaultACSURL = "http://chaospay:8532/epay/3ds/acs"
)

// acsForm — страница мок-ACS: автосабмит обратно на TermUrl, как это делает реальный ACS банка.
var acsForm = template.Must(template.New("acs").Parse(`<!DOCTYPE html>
<html lang="ru">
<head><meta charset="UTF-8"><title>ChaosPay mock ACS</title></head>
<body onload="document.forms[0].submit()">
<p>ChaosPay mock ACS: возвращаемся на {{.TermURL}}</p>
<form action="{{.TermURL}}" method="POST">
	<input type="hidden" name="PaRes" value="{{.PaRes}}">
	<input type="hidden" name="MD" value="{{.MD}}">
	<noscript><button type="submit">Продолжить</button></noscript>
</form>
</body>
</html>`))

// handleACS изображает страницу проверки 3DS банка.
//
// Реальный ACS живет на стороне эмитента: принимает PaReq/MD/TermUrl и после проверки
// сабмитит браузер обратно на TermUrl с PaRes. Без такой страницы 3DS-цикл в локальном
// прогоне не замыкается, потому что в secure3D.action у Halyk стоит внешний адрес.
//
// Исход задается параметром outcome=declined: он влияет только на значение PaRes,
// итог платежа определяет сценарий на POST /api/payment/confirm.
func (c *Controller) handleACS(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "не удалось разобрать форму", http.StatusBadRequest)

		return
	}

	termURL := r.FormValue("TermUrl")
	if termURL == "" {
		http.Error(w, "TermUrl обязателен", http.StatusBadRequest)

		return
	}

	paRes := acsPaResApproved
	if r.URL.Query().Get("outcome") == "declined" {
		paRes = acsPaResDeclined
	}

	data := struct {
		TermURL string
		PaRes   string
		MD      string
	}{
		TermURL: termURL,
		PaRes:   paRes,
		MD:      r.FormValue("MD"),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if err := acsForm.Execute(w, data); err != nil {
		http.Error(w, "не удалось отрисовать страницу", http.StatusInternalServerError)
	}
}
