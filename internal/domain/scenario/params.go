package scenario

import (
	"fmt"
	"strconv"
)

// Param возвращает строковый параметр или дефолт.
func Param(s *Scenario, key, def string) string {
	if s == nil || s.Params == nil {
		return def
	}
	if v, ok := s.Params[key]; ok && v != "" {
		return v
	}
	return def
}

// ParamInt возвращает int-параметр или дефолт.
func ParamInt(s *Scenario, key string, def int) int {
	v := Param(s, key, "")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// ParseNonNegativeInt разбирает целое >= 0; битое или отрицательное значение — ошибка.
func ParseNonNegativeInt(key, value string) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s: want non-negative integer, got %q", key, value)
	}

	return n, nil
}

// ParamNonNegativeInt — как ParamInt, но битое или отрицательное значение — ошибка, а не дефолт.
func ParamNonNegativeInt(s *Scenario, key string, def int) (int, error) {
	v := Param(s, key, "")
	if v == "" {
		return def, nil
	}

	return ParseNonNegativeInt(key, v)
}
