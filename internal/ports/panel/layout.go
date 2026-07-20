package panel

import (
	"fmt"
	"net/http"

	"github.com/vevovip/chaospay/internal/domain/bank"
)

// renderHeader пишет header + bank-tabs + sub-tabs.
// Auto-refresh на cards/log (внутри банка).
func (c *Controller) renderHeader(w http.ResponseWriter, b bank.Bank, tab string) {
	refresh := ""
	refreshNote := "Обновление вручную"
	if tab == "cards" || tab == "log" || b == bank.QR {
		refresh = `<meta http-equiv="refresh" content="3">`
		refreshNote = "Автообновление каждые 3 сек"
	}
	fmt.Fprintf(w, `<!DOCTYPE html>
<html><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
%s
<title>ChaosPay</title>
<style>%s</style>
<script>
function copyText(el, text) {
  navigator.clipboard.writeText(text).then(function() {
    var hint = el.querySelector('.copy-hint');
    if (hint) {
      hint.textContent = 'Copied!';
      hint.classList.add('copied');
      setTimeout(function() {
        hint.textContent = 'click to copy';
        hint.classList.remove('copied');
      }, 1500);
    }
  });
}
function toggleRow(id) {
  var el = document.getElementById('log-detail-' + id);
  if (el) { el.style.display = el.style.display === 'none' ? '' : 'none'; }
}
</script>
</head><body>
<div class="topbar">
  <div class="brand-row">
    <div>
      <h1>ChaosPay</h1>
      <div class="subtitle">Chaos engineering для платёжных интеграций — управление сценариями</div>
    </div>
    <div class="refresh-note">%s</div>
  </div>
  <nav class="tabs bank-tabs" aria-label="Banks">
`, refresh, panelCSS, refreshNote)

	// Bank-level tabs.
	for _, kb := range bank.All {
		active := ""
		if kb == b {
			active = " active"
		}
		title := bank.Titles[kb]
		fmt.Fprintf(w, `<a class="tab%s" href="/panel?bank=%s&tab=%s">%s</a>`, active, kb, defaultTabFor(kb), title)
	}
	active := ""
	if tab == "settings" {
		active = " active"
	}
	fmt.Fprintf(w, `<a class="tab%s" href="/panel?tab=settings">Settings</a>`, active)
	fmt.Fprint(w, `</nav>`)

	// Sub-tabs (внутри банка).
	subtabs := subTabsFor(b)
	if len(subtabs) > 0 {
		fmt.Fprint(w, `<nav class="tabs sub-tabs" aria-label="Sections">`)
		for _, st := range subtabs {
			a := ""
			if st.key == tab {
				a = " active"
			}
			fmt.Fprintf(w, `<a class="tab%s" href="/panel?bank=%s&tab=%s">%s</a>`, a, b, st.key, st.label)
		}
		fmt.Fprint(w, `</nav>`)
	}

	fmt.Fprint(w, `</div>
<main>`)
}

func (c *Controller) renderFooter(w http.ResponseWriter) {
	fmt.Fprint(w, `</main></body></html>`)
}

type subTab struct {
	key, label string
}

// subTabsFor возвращает sub-tabs для каждого банка.
// QR/Loyalty — единая страница без sub-tabs.
func subTabsFor(b bank.Bank) []subTab {
	switch b {
	case bank.Freedom, bank.Epay, bank.Flitt:
		return []subTab{
			{"cards", "Cards"},
			{"scenarios", "Scenarios"},
			{"log", "Request Log"},
		}
	case bank.QR:
		return []subTab{
			{"qr", "QR Codes"},
			{"scenarios", "Scenarios"},
			{"log", "Request Log"},
		}
	case bank.Kaspi:
		return []subTab{
			{"kaspi", "Payments"},
			{"log", "Request Log"},
		}
	case bank.Loyalty:
		return []subTab{
			{"loyalty", "Loyalty"},
			{"log", "Request Log"},
		}
	}
	return nil
}

// defaultTabFor возвращает default sub-tab для каждого банка.
func defaultTabFor(b bank.Bank) string {
	switch b {
	case bank.Freedom, bank.Epay, bank.Flitt:
		return "cards"
	case bank.QR:
		return "qr"
	case bank.Kaspi:
		return "kaspi"
	case bank.Loyalty:
		return "loyalty"
	}
	return ""
}

const panelCSS = `
* { box-sizing: border-box; }
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; margin: 0; background: #f3f6f8; color: #18212b; }
.topbar { background: #fff; padding: 14px 24px 0; box-shadow: 0 1px 8px rgba(24,33,43,0.08); position: sticky; top: 0; z-index: 10; }
.brand-row { display: flex; justify-content: space-between; gap: 16px; align-items: flex-start; margin-bottom: 12px; }
.topbar h1 { margin: 0; color: #18212b; font-size: 22px; line-height: 1.2; }
.subtitle { color: #6a7480; font-size: 13px; margin-top: 3px; }
.refresh-note { color: #6a7480; background: #f3f6f8; border: 1px solid #dde5ec; border-radius: 999px; padding: 6px 10px; font-size: 12px; white-space: nowrap; }
.tabs { display: flex; gap: 2px; overflow-x: auto; }
.bank-tabs { border-bottom: 1px solid #e1e8ee; margin-bottom: 0; }
.bank-tabs .tab { font-size: 15px; padding: 12px 16px; font-weight: 700; }
.sub-tabs { background: #fafbfc; padding: 0 4px; border-bottom: 1px solid #edf1f4; }
.sub-tabs .tab { font-size: 13px; padding: 9px 14px; }
.tab { padding: 11px 14px; color: #53606d; text-decoration: none; font-size: 14px; font-weight: 600; border-bottom: 3px solid transparent; white-space: nowrap; }
.tab:hover { color: #18212b; background: #f7fafc; }
.tab.active { color: #08756f; border-bottom-color: #08756f; }
main { padding: 24px; max-width: 1680px; margin: 0 auto; }
h2 { color: #18212b; margin: 0; font-size: 18px; }
.section-header { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; margin-bottom: 14px; }
.section-title { display: flex; flex-direction: column; gap: 4px; }
.section-title p, .info { color: #64717f; font-size: 13px; margin: 0; line-height: 1.45; }
.info { margin-bottom: 12px; }
code, .info code { background: #e8eef3; padding: 2px 6px; border-radius: 4px; font-size: 12px; }
.toolbar { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; justify-content: flex-end; }
.stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 10px; margin-bottom: 16px; }
.stat { padding: 10px 12px; border-radius: 8px; font-size: 13px; font-weight: 700; color: #18212b; background: #fff; border: 1px solid #dde5ec; box-shadow: 0 1px 2px rgba(24,33,43,0.04); }
.stat-total { border-left: 4px solid #18212b; }
.stat-NEW, .stat-HOLD_PENDING { border-left: 4px solid #74808c; }
.stat-AUTHORIZED { border-left: 4px solid #1c6dd0; }
.stat-CAPTURED, .stat-SUCCESS { border-left: 4px solid #168257; }
.stat-CANCELLED, .stat-EXPIRED { border-left: 4px solid #c26812; }
.stat-REFUNDED, .stat-PARTIAL_REFUNDED { border-left: 4px solid #7752ad; }
.stat-FAILED, .stat-ERROR { border-left: 4px solid #c8323a; }
.stat-SCANNED { border-left: 4px solid #0f8ea8; }
.table-wrap { width: 100%; overflow-x: auto; background: #fff; border: 1px solid #dde5ec; border-radius: 8px; box-shadow: 0 1px 3px rgba(24,33,43,0.06); margin-bottom: 12px; }
table { width: 100%; border-collapse: collapse; min-width: 980px; }
th { background: #f8fafb; color: #53606d; padding: 10px 12px; text-align: left; font-size: 11px; letter-spacing: 0; text-transform: uppercase; border-bottom: 1px solid #dde5ec; }
td { padding: 10px 12px; border-bottom: 1px solid #edf1f4; font-size: 13px; vertical-align: middle; }
tr:last-child td { border-bottom: none; }
tr:hover td { background: #f8fbfb; }
tr.detail td { background: #fbfcfd; padding: 12px; }
.badge { display: inline-block; padding: 4px 9px; border-radius: 999px; color: #18212b; font-size: 11px; font-weight: 700; background: #e8eef3; white-space: nowrap; }
.badge-NEW, .badge-HOLD_PENDING { background: #e8eef3; color: #3f4b56; }
.badge-AUTHORIZED { background: #e6f0ff; color: #1c55a3; }
.badge-CAPTURED, .badge-SUCCESS { background: #e4f5ec; color: #11683f; }
.badge-CANCELLED, .badge-EXPIRED { background: #fff0df; color: #9a4d0b; }
.badge-REFUNDED, .badge-PARTIAL_REFUNDED { background: #f0eafa; color: #5a3b86; }
.badge-FAILED, .badge-ERROR { background: #ffe8ea; color: #a52831; }
.badge-SCANNED { background: #e1f6fa; color: #0b6b7f; }
.bank-badge { display: inline-block; padding: 3px 8px; border-radius: 4px; font-size: 11px; font-weight: 700; background: #eef4f9; color: #1c55a3; text-transform: uppercase; letter-spacing: 0.3px; }
.bank-badge-freedom { background: #edf8f7; color: #08756f; }
.bank-badge-epay { background: #fff5e6; color: #b35400; }
.bank-badge-qr { background: #e1f6fa; color: #0b6b7f; }
.bank-badge-loyalty { background: #f0eafa; color: #5a3b86; }
.actions { display: flex; gap: 6px; flex-wrap: wrap; align-items: center; min-width: 220px; }
.actions form { margin: 0; }
.btn { border: 1px solid transparent; padding: 7px 11px; border-radius: 6px; color: #fff; cursor: pointer; font-size: 12px; font-weight: 700; line-height: 1.15; min-height: 31px; }
.btn:hover { filter: brightness(0.96); }
.btn:disabled { opacity: 0.55; cursor: not-allowed; }
.btn-primary { background: #08756f; }
.btn-success { background: #168257; }
.btn-warning { background: #c26812; }
.btn-danger { background: #c8323a; }
.btn-secondary { background: #5f6b76; }
.btn-purple { background: #7752ad; }
.btn-ghost { background: #fff; color: #53606d; border-color: #cfd8df; }
.empty { text-align: center; padding: 40px 20px; color: #64717f; background: #fff; border: 1px dashed #cfd8df; border-radius: 8px; }
.uuid-cell { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 11px; word-break: break-all; max-width: 240px; cursor: pointer; }
.uuid-cell:hover { background: #edf8f7; }
.copy-hint { font-size: 10px; color: #7b8793; display: block; margin-top: 2px; }
.copied { color: #08756f; font-weight: 700; }
.qr-img { width: 92px; height: 92px; border: 1px solid #dde5ec; border-radius: 6px; background: #fff; }
.panel-card { background: #fff; padding: 16px; border-radius: 8px; margin-bottom: 16px; border: 1px solid #dde5ec; box-shadow: 0 1px 3px rgba(24,33,43,0.05); }
.settings-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 14px; margin-bottom: 16px; }
.status-card { background: #fff; border: 1px solid #dde5ec; border-radius: 8px; padding: 14px; display: flex; flex-direction: column; gap: 8px; }
.status-card strong { font-size: 13px; color: #53606d; }
.status-card .value { font-size: 22px; font-weight: 800; color: #18212b; }
.status-card .hint { color: #64717f; font-size: 12px; line-height: 1.35; }
.endpoint-list { display: grid; gap: 8px; }
.endpoint-row { display: grid; grid-template-columns: 170px minmax(0, 1fr); gap: 10px; align-items: start; font-size: 13px; }
.endpoint-row .key { color: #64717f; }
.endpoint-row .val { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; word-break: break-all; }
.callout { background: #edf8f7; border: 1px solid #cbe8e4; border-left: 4px solid #08756f; border-radius: 8px; padding: 12px 14px; color: #29524f; font-size: 13px; line-height: 1.45; margin-bottom: 16px; }
.next-step { max-width: 220px; color: #53606d; font-size: 12px; line-height: 1.35; }
.next-step strong { color: #18212b; display: block; font-size: 13px; margin-bottom: 2px; }
.scenario-hero { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 16px; align-items: stretch; background: #fff; border: 1px solid #dde5ec; border-left: 4px solid #08756f; border-radius: 8px; padding: 14px 16px; box-shadow: 0 1px 3px rgba(24,33,43,0.05); margin-bottom: 12px; }
.scenario-hero-main { display: flex; flex-direction: column; gap: 4px; justify-content: center; min-width: 0; }
.scenario-hero-main strong { font-size: 17px; color: #18212b; }
.scenario-hero-main span { color: #64717f; font-size: 13px; line-height: 1.45; }
.scenario-metrics { display: grid; grid-template-columns: repeat(4, minmax(82px, 1fr)); gap: 8px; }
.scenario-metric { border: 1px solid #dde5ec; border-radius: 8px; padding: 9px 10px; min-width: 82px; background: #f8fafb; }
.scenario-metric span { display: block; color: #64717f; font-size: 11px; font-weight: 700; margin-bottom: 3px; }
.scenario-metric strong { color: #18212b; font-size: 18px; }
.scenario-flow { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px; margin-bottom: 16px; }
.scenario-flow div { background: #fff; border: 1px solid #dde5ec; border-radius: 8px; padding: 11px 12px; min-width: 0; }
.scenario-flow strong { display: block; color: #18212b; font-size: 13px; margin-bottom: 3px; }
.scenario-flow span { display: block; color: #64717f; font-size: 12px; line-height: 1.35; }
.scenario-form { background: #fff; padding: 16px 0 0; border-radius: 8px; margin-bottom: 0; border: 0; border-top: 1px solid #edf1f4; }
.scenario-form .row { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 12px; margin-bottom: 12px; }
.scenario-form label { display: flex; flex-direction: column; font-size: 12px; color: #53606d; gap: 5px; font-weight: 700; }
.scenario-form input, .scenario-form select, .scenario-form textarea { padding: 8px 9px; border: 1px solid #cfd8df; border-radius: 6px; font-size: 13px; min-height: 36px; background: #fff; }
.scenario-form input:focus, .scenario-form select:focus, .scenario-form textarea:focus { outline: 2px solid #b7e3df; border-color: #08756f; }
.scenario-form button { padding: 8px 18px; }
.preset-card { background: #fff; border: 1px solid #dde5ec; border-radius: 8px; padding: 12px; display: flex; flex-direction: column; gap: 4px; min-width: 0; }
.preset-card-head { display: flex; justify-content: space-between; align-items: center; gap: 8px; padding-bottom: 6px; border-bottom: 1px solid #edf1f4; }
.preset-card-head span { color: #64717f; background: #f3f6f8; border: 1px solid #dde5ec; border-radius: 999px; padding: 2px 7px; font-size: 11px; font-weight: 800; }
.preset-card h3 { margin: 0; font-size: 14px; color: #18212b; }
.preset-card p { margin: 0; font-size: 12px; color: #64717f; line-height: 1.35; }
.preset-option { display: flex; flex-direction: column; gap: 6px; padding: 8px 0; border-top: 1px dashed #e6ebef; }
.preset-card-head + .preset-option { border-top: none; }
.preset-description { min-height: 32px; }
.preset-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.preset-details { font-size: 12px; }
.preset-details summary { cursor: pointer; user-select: none; width: 28px; height: 28px; border-radius: 50%; background: #f3f8f7; color: #08756f; border: 1px solid #cbe8e4; font-weight: 800; list-style: none; display: inline-grid; place-items: center; line-height: 1; }
.preset-details summary span { display: block; font-family: Georgia, "Times New Roman", serif; font-style: italic; font-size: 15px; transform: translateY(-1px); }
.preset-details summary::-webkit-details-marker { display: none; }
.preset-details summary::marker { content: ''; }
.preset-details summary:hover { background: #e6f3f1; border-color: #9fd4ce; }
.preset-details[open] summary { background: #08756f; border-color: #08756f; color: #fff; }
.preset-details pre { margin: 8px 0 0; background: #0f1820; color: #d7e1ea; padding: 10px 12px; border-radius: 6px; font-size: 11px; line-height: 1.45; overflow-x: auto; max-height: 360px; white-space: pre-wrap; word-break: break-word; }
.scenario-presets { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 10px; margin-bottom: 16px; }
.advanced-card summary { display: flex; justify-content: space-between; align-items: center; gap: 12px; cursor: pointer; list-style: none; }
.advanced-card summary::-webkit-details-marker { display: none; }
.advanced-card summary::marker { content: ''; }
.summary-action { color: #08756f; background: #edf8f7; border: 1px solid #cbe8e4; border-radius: 999px; padding: 6px 10px; font-size: 0; font-weight: 800; white-space: nowrap; }
.summary-action::after { content: "Open form"; font-size: 12px; }
.advanced-card[open] .summary-action { color: #53606d; background: #f3f6f8; border-color: #dde5ec; }
.advanced-card[open] .summary-action::after { content: "Close form"; }
.scenario-note { background: #fbfcfd; }
.scenario-rule { display: flex; align-items: center; gap: 7px; flex-wrap: wrap; }
.endpoint-pill { display: inline-block; color: #29524f; background: #edf8f7; border: 1px solid #cbe8e4; border-radius: 999px; padding: 4px 8px; font-size: 12px; font-weight: 800; white-space: nowrap; }
.match-stack { display: flex; flex-direction: column; gap: 3px; min-width: 160px; }
.match-stack span { color: #64717f; font-size: 12px; }
.signature-ok { color: #11683f; font-weight: 800; }
.signature-bad { color: #a52831; font-weight: 800; }
pre.body { background: #17212b; color: #d7e1ea; padding: 12px; border-radius: 6px; font-size: 11px; overflow-x: auto; max-height: 340px; margin: 0; }
.kv { display: grid; grid-template-columns: 220px minmax(0, 1fr); gap: 8px 16px; font-size: 13px; }
.kv .key { color: #64717f; }
.kv .val { font-family: monospace; }
.muted { color: #7b8793; font-size: 11px; }
.money { font-weight: 800; white-space: nowrap; }
.nowrap { white-space: nowrap; }
.danger-zone { border-color: #ffd4d8; background: #fffafa; }
.status-http-ok { color: #11683f; font-weight: 800; }
.status-http-warn { color: #9a4d0b; font-weight: 800; }
.status-http-bad { color: #a52831; font-weight: 800; }
@media (max-width: 720px) {
  .topbar { padding: 12px 14px 0; }
  .brand-row, .section-header { flex-direction: column; align-items: stretch; }
  .refresh-note { white-space: normal; }
  main { padding: 16px 12px; }
  .scenario-hero { grid-template-columns: 1fr; }
  .scenario-metrics, .scenario-flow { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .scenario-flow div:last-child { grid-column: 1 / -1; }
  .kv { grid-template-columns: 1fr; }
  .endpoint-row { grid-template-columns: 1fr; gap: 3px; }
  .toolbar { justify-content: flex-start; }
}
`
