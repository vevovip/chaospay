package epay

import (
	"net/http"
)

// paymentAPIScript — заглушка виджета платёжной страницы Halyk.
//
// Реальный виджет подключается страницей мерчанта скриптом payment-api.js, показывает
// форму карты и сам отправляет платёж в банк. Мок повторяет контракт: определяет
// halyk.showPaymentWidget, рисует форму и отправляет cryptopay с переданным токеном —
// без этого оплату новой картой и привязку карты нельзя пройти локально.
const paymentAPIScript = `(function () {
  var BASE_URL = window.location.origin;

  function form(payment, onResult) {
    var box = document.createElement('div');
    box.setAttribute('style', 'max-width:420px;margin:24px auto;padding:20px;border:1px solid #e3e6e8;border-radius:12px;font-family:-apple-system,Segoe UI,Roboto,sans-serif');
    box.innerHTML =
      '<h3 style="margin:0 0 4px">ChaosPay: форма оплаты Halyk</h3>' +
      '<p style="margin:0 0 16px;color:#5c6367;font-size:13px">invoice ' + payment.invoiceId +
      ' · ' + (payment.amount / 100).toFixed(2) + ' ' + (payment.currency || 'KZT') + '</p>' +
      '<input id="cp-pan" value="4405639704015096" style="width:100%;padding:10px;margin-bottom:8px;border:1px solid #d5d9dc;border-radius:8px">' +
      '<input id="cp-exp" value="12/26" style="width:48%;padding:10px;border:1px solid #d5d9dc;border-radius:8px">' +
      '<input id="cp-cvc" value="123" style="width:48%;padding:10px;float:right;border:1px solid #d5d9dc;border-radius:8px">' +
      '<button id="cp-pay" style="width:100%;margin-top:16px;padding:12px;border:0;border-radius:8px;background:#c83f72;color:#fff;font-size:15px">Оплатить</button>' +
      '<div id="cp-status" style="margin-top:12px;font-size:13px;color:#5c6367"></div>';
    document.body.appendChild(box);

    document.getElementById('cp-pay').onclick = function () {
      var status = document.getElementById('cp-status');
      status.textContent = 'Отправляем в банк…';

      var body = {
        amount: payment.amount,
        currency: payment.currency || 'KZT',
        invoiceId: payment.invoiceId,
        terminalId: payment.terminal,
        accountId: payment.accountId,
        description: payment.description,
        postlink: payment.postLink,
        failurePostlink: payment.failurePostLink,
        backlink: payment.backLink,
        failureBacklink: payment.failureBackLink,
        cardSave: !!payment.cardSave,
        cryptogram: btoa(document.getElementById('cp-pan').value + '|' +
          document.getElementById('cp-exp').value + '|' +
          document.getElementById('cp-cvc').value)
      };

      if (payment.paymentType) { body.paymentType = payment.paymentType; }
      if (payment.postLinkBind) { body.postLinkBind = payment.postLinkBind; }

      fetch(BASE_URL + '/api/payment/cryptopay', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': (payment.auth.token_type || 'Bearer') + ' ' + payment.auth.access_token
        },
        body: JSON.stringify(body)
      }).then(function (r) { return r.json().then(function (data) { return { ok: r.ok, data: data }; }); })
        .then(function (res) {
          status.textContent = res.ok ? 'Банк принял платёж' : ('Отказ банка: ' + (res.data.message || ''));
          if (res.ok && res.data.secure3D && res.data.secure3D.action) {
            status.textContent = 'Требуется подтверждение 3DS';
          }
          if (typeof onResult === 'function') { onResult(res.data); }
        })
        .catch(function (err) { status.textContent = 'Ошибка сети: ' + err; });
    };
  }

  window.halyk = {
    showPaymentWidget: function (payment, onResult) { form(payment, onResult); },
    pay: function (payment) { form(payment, null); }
  };
})();`

// handlePaymentAPI отдаёт скрипт виджета платёжной страницы.
func (c *Controller) handlePaymentAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	_, _ = w.Write([]byte(paymentAPIScript))
}
