package reminder

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestNotificationIsSilentAndPreservesSpecialCharacters(t *testing.T) {
	content := "中文 & <tag> \"quote\" ' ]]> $([not executable])\n第二行 😀"
	payload, err := notificationXML("桌面提醒", content)
	if err != nil {
		t.Fatal(err)
	}
	var toast struct {
		Texts []string `xml:"visual>binding>text"`
		Audio struct {
			Silent string `xml:"silent,attr"`
			Source string `xml:"src,attr"`
		} `xml:"audio"`
	}
	if err := xml.Unmarshal([]byte(payload), &toast); err != nil {
		t.Fatal(err)
	}
	if len(toast.Texts) != 2 || toast.Texts[1] != content || toast.Audio.Silent != "true" || toast.Audio.Source != "" {
		t.Fatalf("invalid notification: %+v", toast)
	}
	if strings.Contains(payload, "<![CDATA[") {
		t.Fatal("unsafe CDATA use")
	}
}
