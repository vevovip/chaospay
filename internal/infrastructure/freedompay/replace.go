package freedompay

import "strings"

// ReplaceTagValue заменяет содержимое тега в готовом XML-теле (для invalid_signature scenario).
func ReplaceTagValue(body, tag, value string) string {
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	startIdx := strings.Index(body, open)
	if startIdx == -1 {
		return body
	}
	endIdx := strings.Index(body, close)
	if endIdx == -1 || endIdx < startIdx {
		return body
	}
	return body[:startIdx+len(open)] + value + body[endIdx:]
}

// RemoveTag удаляет тег целиком (открывающий, содержимое, закрывающий) из готового XML-тела.
// Нужен для missing_field на поля, которые добавляет signedXML (pg_sig, pg_salt) уже после applyScenarioAfter.
func RemoveTag(body, tag string) string {
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	startIdx := strings.Index(body, open)
	if startIdx == -1 {
		return body
	}
	endIdx := strings.Index(body[startIdx:], close)
	if endIdx == -1 {
		return body
	}
	return body[:startIdx] + body[startIdx+endIdx+len(close):]
}
