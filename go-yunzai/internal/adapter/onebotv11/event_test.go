package onebotv11

import "testing"

func TestDecodeEventCategories(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "message", input: `{"time":1,"self_id":9000,"post_type":"message","message_type":"group","sub_type":"normal","message_id":12,"user_id":1001,"group_id":2001,"message":[{"type":"text","data":{"text":"hello"}},{"type":"future_segment","data":{"new_field":42}}],"sender":{"user_id":1001,"nickname":"tester"},"unknown_field":"ignored"}`},
		{name: "notice", input: `{"post_type":"notice","notice_type":"notify","sub_type":"poke","self_id":9000,"user_id":1001,"target_id":9000,"group_id":2001}`},
		{name: "request", input: `{"post_type":"request","request_type":"friend","user_id":1001,"flag":"friend-flag","comment":"synthetic"}`},
		{name: "heartbeat", input: `{"post_type":"meta_event","meta_event_type":"heartbeat","self_id":9000,"interval":5000,"status":{"online":true,"good":true}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evt, err := DecodeEvent([]byte(tt.input))
			if err != nil {
				t.Fatalf("DecodeEvent() error = %v", err)
			}
			switch tt.name {
			case "message":
				if evt.MessageType != "group" || evt.GroupID != 2001 || evt.Sender.Nickname != "tester" {
					t.Fatalf("unexpected message event: %+v", evt)
				}
				if len(evt.Message) != 2 || evt.Message[1].Type != "future_segment" || evt.Message[1].Data["new_field"] != float64(42) {
					t.Fatalf("array segments were not preserved: %+v", evt.Message)
				}
			case "notice":
				if evt.NoticeType != "notify" || evt.SubType != "poke" || evt.TargetID != 9000 {
					t.Fatalf("unexpected notice event: %+v", evt)
				}
			case "request":
				if evt.RequestType != "friend" || evt.Flag != "friend-flag" {
					t.Fatalf("unexpected request event: %+v", evt)
				}
			case "heartbeat":
				if evt.MetaEventType != "heartbeat" || evt.Interval != 5000 || evt.Status["online"] != true {
					t.Fatalf("unexpected heartbeat event: %+v", evt)
				}
			}
		})
	}
}

func TestDecodeEventRejectsNonArrayMessage(t *testing.T) {
	_, err := DecodeEvent([]byte(`{"post_type":"message","message_type":"private","user_id":1001,"message":"string-form"}`))
	if err == nil {
		t.Fatal("DecodeEvent() accepted string message format")
	}
}

func TestDecodeEventRejectsEmptyArrayMessage(t *testing.T) {
	for _, raw := range []string{
		`{"post_type":"message","message_type":"private","user_id":1001,"message":null}`,
		`{"post_type":"message","message_type":"private","user_id":1001,"message":[]}`,
	} {
		if _, err := DecodeEvent([]byte(raw)); err == nil {
			t.Fatalf("DecodeEvent() accepted empty message payload %q", raw)
		}
	}
}

func TestDecodeEventRequiresPostType(t *testing.T) {
	if _, err := DecodeEvent([]byte(`{"self_id":9000}`)); err == nil {
		t.Fatal("DecodeEvent() accepted event without post_type")
	}
}
