package scenario

import "strconv"

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
