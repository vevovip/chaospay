package freedompay

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// XMLPrefix — заголовок ответа банка.
const XMLPrefix = `<?xml version="1.0" encoding="utf-8"?>`

// ParsedRequest — упрощённое представление входящего XML.
type ParsedRequest struct {
	Fields OrdMap
}

// Get — строковое значение поля или дефолт.
func (p *ParsedRequest) Get(key, def string) string {
	if v, ok := p.Fields.Get(key); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return def
}

// Has — есть ли непустое поле.
func (p *ParsedRequest) Has(key string) bool {
	v, ok := p.Fields.Get(key)
	if !ok {
		return false
	}
	if s, ok := v.(string); ok {
		return s != ""
	}
	return true
}

// FieldsForSignature возвращает копию полей без pg_sig (для верификации подписи).
func (p *ParsedRequest) FieldsForSignature() OrdMap {
	return p.Fields.WithoutKey("pg_sig")
}

// ParseRequestXML парсит <request>...</request> в плоский OrdMap.
// Вложенные элементы (массив submap, например receipt_positions) собираются в []OrdMap.
func ParseRequestXML(xmlStr string) (*ParsedRequest, error) {
	xmlStr = strings.TrimSpace(xmlStr)
	if xmlStr == "" {
		return &ParsedRequest{}, nil
	}

	dec := xml.NewDecoder(strings.NewReader(xmlStr))
	root, err := decodeRoot(dec)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return &ParsedRequest{}, nil
	}
	pr := &ParsedRequest{Fields: make(OrdMap, 0, len(*root))}
	for _, kv := range *root {
		pr.Fields = pr.Fields.Set(kv.Key, kv.Value)
	}
	return pr, nil
}

func decodeRoot(dec *xml.Decoder) (*OrdMap, error) {
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		switch tok.(type) {
		case xml.StartElement:
			return decodeChildren(dec)
		}
	}
}

// decodeChildren возвращает все дочерние элементы текущего открытого тега.
// Если один и тот же тег встречается несколько раз — собирает в []OrdMap.
func decodeChildren(dec *xml.Decoder) (*OrdMap, error) {
	out := OrdMap{}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return &out, nil
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name := t.Name.Local
			value, err := decodeElementValue(dec)
			if err != nil {
				return nil, err
			}
			if existing, ok := out.Get(name); ok {
				switch ev := existing.(type) {
				case OrdMap:
					if newMap, ok := value.(OrdMap); ok {
						out = out.Set(name, []OrdMap{ev, newMap})
					}
				case []OrdMap:
					if newMap, ok := value.(OrdMap); ok {
						out = out.Set(name, append(ev, newMap))
					}
				default:
					out = out.Set(name, value)
				}
				continue
			}
			out = out.Set(name, value)
		case xml.EndElement:
			return &out, nil
		}
	}
}

// decodeElementValue читает содержимое элемента: либо строка (CharData), либо вложенные элементы (OrdMap).
func decodeElementValue(dec *xml.Decoder) (any, error) {
	var text strings.Builder
	hasChildren := false
	children := OrdMap{}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.CharData:
			if !hasChildren {
				text.Write(t)
			}
		case xml.StartElement:
			hasChildren = true
			name := t.Name.Local
			val, err := decodeElementValue(dec)
			if err != nil {
				return nil, err
			}
			if existing, ok := children.Get(name); ok {
				switch ev := existing.(type) {
				case OrdMap:
					if newMap, ok := val.(OrdMap); ok {
						children = children.Set(name, []OrdMap{ev, newMap})
					}
				case []OrdMap:
					if newMap, ok := val.(OrdMap); ok {
						children = children.Set(name, append(ev, newMap))
					}
				default:
					children = children.Set(name, val)
				}
			} else {
				children = children.Set(name, val)
			}
		case xml.EndElement:
			if hasChildren {
				return children, nil
			}
			return strings.TrimSpace(text.String()), nil
		}
	}
	if hasChildren {
		return children, nil
	}
	return strings.TrimSpace(text.String()), nil
}

// RenderResponse рендерит OrdMap в XML <root>...</root>.
// Для скаляров — <key>value</key>. Для []OrdMap — повторяющиеся теги. Для вложенной OrdMap — <key>...</key>.
func RenderResponse(rootName string, fields OrdMap) string {
	var b strings.Builder
	b.WriteString(XMLPrefix)
	b.WriteString("<")
	b.WriteString(rootName)
	b.WriteString(">")
	renderFields(&b, fields)
	b.WriteString("</")
	b.WriteString(rootName)
	b.WriteString(">")
	return b.String()
}

func renderFields(b *strings.Builder, fields OrdMap) {
	for _, kv := range fields {
		switch v := kv.Value.(type) {
		case string:
			fmt.Fprintf(b, "<%s>%s</%s>", kv.Key, escapeXML(v), kv.Key)
		case OrdMap:
			fmt.Fprintf(b, "<%s>", kv.Key)
			renderFields(b, v)
			fmt.Fprintf(b, "</%s>", kv.Key)
		case []OrdMap:
			for _, item := range v {
				fmt.Fprintf(b, "<%s>", kv.Key)
				renderFields(b, item)
				fmt.Fprintf(b, "</%s>", kv.Key)
			}
		default:
			fmt.Fprintf(b, "<%s>%v</%s>", kv.Key, v, kv.Key)
		}
	}
}

func escapeXML(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		`'`, "&apos;",
	)
	return r.Replace(s)
}

// ScriptNameFromURL извлекает имя скрипта из пути URL (последний сегмент).
//
// Для XML-эндпоинтов:
//
//	/v1/merchant/{id}/card/init           → "init"
//	/v1/merchant/{id}/card/direct         → "direct"
//	/get_status3.php                      → "get_status3.php"
//	/do_capture.php                       → "do_capture.php"
//	/cancel.php                           → "cancel.php"
//	/revoke.php                           → "revoke.php"
//	/init_payment.php                     → "init_payment.php"
//	/v1/merchant/{id}/cardstorage/add2    → "add2"
//	/v1/merchant/{id}/cardstorage/remove  → "remove"
//	/api/v1/payment-gateway/webhook/freedompay      → "freedompay"
//	/api/v1/payment-gateway/webhook/freedompay/card → "card"
func ScriptNameFromURL(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}
