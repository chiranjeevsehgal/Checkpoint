package model

import "testing"

func TestValidateRejectsMalformedIDs(t *testing.T) {
	valid := TranscriptionRequestedEvent{
		Data: TranscriptionRequestedData{
			AudioID: "11111111-1111-1111-1111-111111111111",
			UserID:  "22222222-2222-2222-2222-222222222222",
		},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid ids must pass: %v", err)
	}

	cases := map[string]TranscriptionRequestedEvent{
		"bad audio id": {Data: TranscriptionRequestedData{AudioID: "not-a-uuid", UserID: valid.Data.UserID}},
		"bad user id":  {Data: TranscriptionRequestedData{AudioID: valid.Data.AudioID, UserID: ""}},
	}
	for name, event := range cases {
		t.Run(name, func(t *testing.T) {
			if err := event.Validate(); err == nil {
				t.Fatal("malformed id must be rejected")
			}
		})
	}
}
