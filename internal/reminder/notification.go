package reminder

import (
	"bytes"
	"encoding/xml"
)

// 使用 XML 转义，不把提醒文本放入 CDATA 或可执行脚本。
func notificationXML(title, content string) (string, error) {
	var b bytes.Buffer
	b.WriteString(`<toast duration="short"><visual><binding template="ToastGeneric"><text>`)
	if err := xml.EscapeText(&b, []byte(title)); err != nil {
		return "", err
	}
	b.WriteString(`</text><text>`)
	if err := xml.EscapeText(&b, []byte(content)); err != nil {
		return "", err
	}
	b.WriteString(`</text></binding></visual><audio silent="true" /></toast>`)
	return b.String(), nil
}
