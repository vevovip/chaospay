package flitt

import (
	"testing"

	"github.com/vevovip/chaospay/internal/domain/scenario"
	infraflitt "github.com/vevovip/chaospay/internal/infrastructure/flitt"
)

// applyScenarioAfter — экспортируем через unexported alias-helper для теста.
// (Сам applyScenarioAfter — пакетная функция в scenario.go.)

func TestApplyScenarioAfter_WrongPaymentID(t *testing.T) {
	t.Parallel()
	sc := &scenario.Scenario{
		Action: scenario.ActionWrongPaymentID,
		Params: map[string]string{"payment_id": "777"},
	}

	// Direct
	direct := infraflitt.DirectEnvelope{Response: infraflitt.DirectResponse{PaymentID: int64(1)}}
	out := applyScenarioAfter(sc, direct).(infraflitt.DirectEnvelope)
	if out.Response.PaymentID != int64(777) {
		t.Fatalf("direct payment_id: got %v, want 777", out.Response.PaymentID)
	}

	// Recurring
	rec := infraflitt.RecurringEnvelope{Response: infraflitt.RecurringResponse{PaymentID: int64(1)}}
	out2 := applyScenarioAfter(sc, rec).(infraflitt.RecurringEnvelope)
	if out2.Response.PaymentID != int64(777) {
		t.Fatalf("recurring payment_id: got %v, want 777", out2.Response.PaymentID)
	}

	// Status
	st := infraflitt.StatusEnvelope{Response: infraflitt.StatusResponse{PaymentID: 1}}
	out3 := applyScenarioAfter(sc, st).(infraflitt.StatusEnvelope)
	if out3.Response.PaymentID != 777 {
		t.Fatalf("status payment_id: got %v, want 777", out3.Response.PaymentID)
	}
}

func TestApplyScenarioAfter_WrongAmount(t *testing.T) {
	t.Parallel()
	sc := &scenario.Scenario{
		Action: scenario.ActionWrongAmount,
		Params: map[string]string{"amount": "1"},
	}

	st := infraflitt.StatusEnvelope{Response: infraflitt.StatusResponse{
		Amount:           "5000",
		ActualAmount:     "5000",
		SettlementAmount: "5000",
	}}
	out := applyScenarioAfter(sc, st).(infraflitt.StatusEnvelope)
	if out.Response.Amount != "1" || out.Response.ActualAmount != "1" || out.Response.SettlementAmount != "1" {
		t.Fatalf("amounts not overridden: %+v", out.Response)
	}
}

func TestApplyScenarioAfter_MissingField(t *testing.T) {
	t.Parallel()
	cases := []struct {
		field string
		check func(r infraflitt.StatusResponse) bool
	}{
		{"signature", func(r infraflitt.StatusResponse) bool { return r.Signature == "" }},
		{"approval_code", func(r infraflitt.StatusResponse) bool { return r.ApprovalCode == "" }},
		{"rrn", func(r infraflitt.StatusResponse) bool { return r.RRN == "" }},
		{"masked_card", func(r infraflitt.StatusResponse) bool { return r.MaskedCard == "" }},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.field, func(t *testing.T) {
			t.Parallel()
			sc := &scenario.Scenario{
				Action: scenario.ActionMissingField,
				Params: map[string]string{"field": tc.field},
			}
			st := infraflitt.StatusEnvelope{Response: infraflitt.StatusResponse{
				Signature:    "sig",
				ApprovalCode: "ap",
				RRN:          "rrn",
				MaskedCard:   "1111",
			}}
			out := applyScenarioAfter(sc, st).(infraflitt.StatusEnvelope)
			if !tc.check(out.Response) {
				t.Fatalf("field %s not stripped: %+v", tc.field, out.Response)
			}
		})
	}
}

func TestApplyScenarioAfter_ForceStatus(t *testing.T) {
	t.Parallel()
	sc := &scenario.Scenario{
		Action: scenario.ActionForceStatus,
		Params: map[string]string{"order_status": infraflitt.OrderStatusApproved},
	}
	st := infraflitt.StatusEnvelope{Response: infraflitt.StatusResponse{
		OrderStatus:    infraflitt.OrderStatusDeclined,
		ResponseStatus: infraflitt.ResponseStatusFailure,
	}}
	out := applyScenarioAfter(sc, st).(infraflitt.StatusEnvelope)
	if out.Response.OrderStatus != infraflitt.OrderStatusApproved {
		t.Fatalf("order_status: got %s", out.Response.OrderStatus)
	}
	if out.Response.ResponseStatus != infraflitt.ResponseStatusSuccess {
		t.Fatalf("response_status: got %s", out.Response.ResponseStatus)
	}

	// Декларативный decline override
	sc2 := &scenario.Scenario{
		Action: scenario.ActionForceStatus,
		Params: map[string]string{"order_status": infraflitt.OrderStatusDeclined},
	}
	st2 := infraflitt.StatusEnvelope{Response: infraflitt.StatusResponse{
		OrderStatus:    infraflitt.OrderStatusApproved,
		ResponseStatus: infraflitt.ResponseStatusSuccess,
	}}
	out2 := applyScenarioAfter(sc2, st2).(infraflitt.StatusEnvelope)
	if out2.Response.ResponseStatus != infraflitt.ResponseStatusFailure {
		t.Fatalf("decline override: response_status: got %s", out2.Response.ResponseStatus)
	}
}

func TestApplyScenarioAfter_PassThrough(t *testing.T) {
	t.Parallel()
	// Scenario action не из списка модификаторов — возврат без изменений.
	sc := &scenario.Scenario{Action: scenario.ActionDelay}
	in := infraflitt.StatusEnvelope{Response: infraflitt.StatusResponse{Amount: "100"}}
	out := applyScenarioAfter(sc, in)
	got, ok := out.(infraflitt.StatusEnvelope)
	if !ok {
		t.Fatalf("type changed unexpectedly: %T", out)
	}
	if got.Response.Amount != "100" {
		t.Fatalf("amount mutated: %+v", got.Response)
	}
}

func TestFlittOutcomeFromScenario(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		sc   *scenario.Scenario
		want infraflitt.CardOutcome
	}{
		{"nil → approved", nil, infraflitt.OutcomeApproved},
		{"force_failure default → declined",
			&scenario.Scenario{Action: scenario.ActionForceFailure},
			infraflitt.OutcomeDeclined,
		},
		{"force_failure with outcome param",
			&scenario.Scenario{Action: scenario.ActionForceFailure, Params: map[string]string{"outcome": "insufficient_funds"}},
			infraflitt.OutcomeInsufficientFunds,
		},
		{"force_3ds default → approved_3ds",
			&scenario.Scenario{Action: scenario.ActionForce3DS},
			infraflitt.OutcomeApproved3DS,
		},
		{"force_3ds with outcome=declined_3ds",
			&scenario.Scenario{Action: scenario.ActionForce3DS, Params: map[string]string{"outcome": "declined_3ds"}},
			infraflitt.OutcomeDeclined3DS,
		},
		{"unrelated action → approved",
			&scenario.Scenario{Action: scenario.ActionDelay},
			infraflitt.OutcomeApproved,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := flittOutcomeFromScenario(tc.sc); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestUintFromInt(t *testing.T) {
	t.Parallel()
	if got := uintFromInt(5); got != 5 {
		t.Fatalf("positive: got %d", got)
	}
	if got := uintFromInt(0); got != 0 {
		t.Fatalf("zero: got %d", got)
	}
	if got := uintFromInt(-10); got != 0 {
		t.Fatalf("negative: got %d, want 0", got)
	}
}

func TestDefaultStr(t *testing.T) {
	t.Parallel()
	if got := defaultStr("", "fallback"); got != "fallback" {
		t.Fatalf("empty: got %s", got)
	}
	if got := defaultStr("present", "fallback"); got != "present" {
		t.Fatalf("non-empty: got %s", got)
	}
}
