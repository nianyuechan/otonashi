package message

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestImagePNGUsesOneBotBase64File(t *testing.T) {
	pngData := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a}
	segment := ImagePNG(pngData)

	if segment.Type != "image" {
		t.Fatalf("segment type = %q, want image", segment.Type)
	}
	file, ok := segment.Data["file"].(string)
	if !ok {
		t.Fatalf("image file has unexpected type: %#v", segment.Data["file"])
	}
	if file != "base64://"+base64.StdEncoding.EncodeToString(pngData) {
		t.Fatalf("image file = %q", file)
	}

	payload, err := json.Marshal(segment)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if string(payload) != `{"type":"image","data":{"file":"base64://iVBORw0K"}}` {
		t.Fatalf("serialized segment = %s", payload)
	}
}
