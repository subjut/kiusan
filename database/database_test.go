package database

import (
	"reflect"
	"testing"
	"time"
)

// The JSON-in-TEXT list types persist through Value/Scan; these tests pin
// the codec contract: round trips preserve the content, nil serializes to
// the empty document, and malformed or mistyped input fails the scan.

func TestTimeSliceCodec(t *testing.T) {
	in := TimeSlice{
		time.Date(2026, 5, 4, 9, 30, 0, 0, time.UTC),
		time.Date(2026, 5, 5, 15, 0, 0, 0, time.FixedZone("CEST", 2*3600)),
	}
	val, err := in.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	var out TimeSlice
	if err := out.Scan(val); err != nil {
		t.Fatalf("Scan(Value): %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("round trip: %d times, want %d", len(out), len(in))
	}
	for i := range in {
		if !out[i].Equal(in[i]) {
			t.Fatalf("round trip: time %d = %v, want %v", i, out[i], in[i])
		}
	}
}

func TestTimeSliceScan(t *testing.T) {
	var out TimeSlice
	if err := out.Scan(nil); err != nil || out != nil {
		t.Fatalf("Scan(nil): err %v, out %v — want nil, nil", err, out)
	}
	if err := out.Scan([]byte(`["2026-05-04T09:30:00Z"]`)); err != nil || len(out) != 1 {
		t.Fatalf("Scan(bytes): err %v, out %v", err, out)
	}
	if err := out.Scan(`["2026-05-04T09:30:00Z"]`); err != nil || len(out) != 1 {
		t.Fatalf("Scan(string): err %v, out %v", err, out)
	}
	if err := out.Scan([]byte(`{nope`)); err == nil {
		t.Fatalf("Scan(malformed): expected an error")
	}
	if err := out.Scan(42); err == nil {
		t.Fatalf("Scan(int): expected an error for a mistyped value")
	}
}

func TestNilListValue(t *testing.T) {
	check := func(name string, v any, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s nil Value: %v", name, err)
		}
		if v != "[]" {
			t.Fatalf("%s nil Value: %q, want \"[]\"", name, v)
		}
	}
	v, err := (TimeSlice)(nil).Value()
	check("TimeSlice", v, err)
	v, err = (AnswerList)(nil).Value()
	check("AnswerList", v, err)
	v, err = (ChoiceList)(nil).Value()
	check("ChoiceList", v, err)
	v, err = (VoteAnswerList)(nil).Value()
	check("VoteAnswerList", v, err)
}

func TestAnswerListCodec(t *testing.T) {
	in := AnswerList{{
		ID:        "id-1",
		Name:      "Alice",
		Marks:     map[string]string{"k1": "g", "k2": "y"},
		UpdatedAt: time.Date(2026, 5, 4, 9, 30, 0, 0, time.UTC),
	}}
	val, err := in.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	var out AnswerList
	if err := out.Scan(val); err != nil {
		t.Fatalf("Scan(Value): %v", err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Fatalf("round trip: %+v, want %+v", out, in)
	}

	// The empty document and nil both scan to a nil list.
	var empty AnswerList
	if err := empty.Scan(""); err != nil || empty != nil {
		t.Fatalf("Scan(\"\"): err %v, out %v — want nil", err, empty)
	}
	if err := empty.Scan(nil); err != nil || empty != nil {
		t.Fatalf("Scan(nil): err %v, out %v — want nil", err, empty)
	}
	if err := empty.Scan([]byte(`not json`)); err == nil {
		t.Fatalf("Scan(malformed): expected an error")
	}
	if err := empty.Scan(3.14); err == nil {
		t.Fatalf("Scan(float): expected an error for a mistyped value")
	}
}

func TestChoiceListCodec(t *testing.T) {
	in := ChoiceList{{ID: "c1", Text: "A"}, {ID: "c2", Text: "B", Image: "img"}}
	val, err := in.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	var out ChoiceList
	if err := out.Scan(val); err != nil {
		t.Fatalf("Scan(Value): %v", err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Fatalf("round trip: %+v, want %+v", out, in)
	}
	var nilList ChoiceList
	if err := nilList.Scan(nil); err != nil || nilList != nil {
		t.Fatalf("Scan(nil): err %v, out %v — want nil", err, nilList)
	}
	if err := nilList.Scan([]byte(`[1,2]`)); err == nil {
		t.Fatalf("Scan(wrong element type): expected an error")
	}
}

func TestVoteAnswerListCodec(t *testing.T) {
	in := VoteAnswerList{
		{
			ID:        "u1",
			Name:      "Ada",
			Notes:     map[string]string{"c1": "has food"},
			Marks:     map[string]string{"c2": "y"},
			UpdatedAt: time.Date(2026, 5, 4, 9, 30, 0, 0, time.UTC),
		},
	}
	val, err := in.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	var out VoteAnswerList
	if err := out.Scan(val); err != nil {
		t.Fatalf("Scan(Value): %v", err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Fatalf("round trip: %+v, want %+v", out, in)
	}
	var empty VoteAnswerList
	if err := empty.Scan(""); err != nil || empty != nil {
		t.Fatalf("Scan(\"\"): err %v, out %v — want nil", err, empty)
	}
	if err := empty.Scan([]byte(`{`)); err == nil {
		t.Fatalf("Scan(malformed): expected an error")
	}
}
