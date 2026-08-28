package pgclient

// SecretResolver отдает ключ кабинета по его номеру. Один мок обслуживает несколько
// кабинетов, и постлинк подписывается ключом того, в котором прошел платеж.
type SecretResolver func(merchantID uint) string

// StaticSecret — резолвер с единственным ключом.
func StaticSecret(secret string) SecretResolver {
	return func(uint) string { return secret }
}
