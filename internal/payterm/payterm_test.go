package payterm

import (
	"testing"
	"time"

	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/holiday"
	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/jpdate"
)

func d(s string) jpdate.Date {
	v, err := jpdate.Parse(s)
	if err != nil {
		panic(err)
	}
	return v
}

func TestDue(t *testing.T) {
	cases := []struct {
		name    string
		txn     string
		terms   Terms
		closing string
		pay     string
		label   string
	}{
		{"月末締め翌月末払い", "2026-10-20", Terms{31, 1, 31, ""}, "2026-10-31", "2026-11-30", "月末締め翌月末払い"},
		{"2月末が土曜→前営業日", "2026-01-15", Terms{31, 1, 31, "previous"}, "2026-01-31", "2026-02-27", ""},
		{"2月末が土曜→翌営業日", "2026-01-15", Terms{31, 1, 31, "next"}, "2026-01-31", "2026-03-02", ""},
		{"20日締め翌月10日払い・締め後", "2026-10-21", Terms{20, 1, 10, ""}, "2026-11-20", "2026-12-10", "20日締め翌月10日払い"},
		{"20日締め当日", "2026-10-20", Terms{20, 1, 10, ""}, "2026-10-20", "2026-11-10", ""},
		{"翌々月5日が連休中→前営業日", "2026-03-10", Terms{31, 2, 5, "previous"}, "2026-03-31", "2026-05-01", "月末締め翌々月5日払い"},
		{"翌々月5日が連休中→翌営業日", "2026-03-10", Terms{31, 2, 5, "next"}, "2026-03-31", "2026-05-07", ""},
		{"そのまま", "2026-03-10", Terms{31, 2, 5, "none"}, "2026-03-31", "2026-05-05", ""},
		{"年またぎ", "2026-12-25", Terms{20, 1, 31, ""}, "2027-01-20", "2027-02-26", ""},
		{"当月末払い", "2026-09-10", Terms{15, 0, 31, ""}, "2026-09-15", "2026-09-30", "15日締め当月末払い"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := Due(d(c.txn), c.terms, nil)
			if err != nil {
				t.Fatal(err)
			}
			if r.ClosingDate.String() != c.closing || r.PayDate.String() != c.pay {
				t.Fatalf("締め %s 支払 %s（want %s %s）%s", r.ClosingDate, r.PayDate, c.closing, c.pay, r.AdjustReason)
			}
			if c.label != "" && r.TermsLabel != c.label {
				t.Fatalf("label %q", r.TermsLabel)
			}
			if r.Adjusted != (r.PayDate != r.ScheduledDate) || (r.Adjusted && r.AdjustReason == "") {
				t.Fatalf("adjusted の整合: %+v", r)
			}
		})
	}
}

func TestDueWithCompanyHolidays(t *testing.T) {
	cal := &holiday.Calendar{Annual: []holiday.MonthDay{{Month: time.December, Day: 30}, {Month: time.December, Day: 31}}}
	r, err := Due(d("2026-11-10"), Terms{31, 1, 31, "previous"}, cal)
	if err != nil {
		t.Fatal(err)
	}
	if r.PayDate.String() != "2026-12-29" {
		t.Fatalf("%s %s", r.PayDate, r.AdjustReason)
	}
}

func TestDueErrors(t *testing.T) {
	for _, tm := range []Terms{{0, 1, 31, ""}, {29, 1, 31, ""}, {31, 1, 0, ""}, {31, -1, 31, ""}, {31, 13, 31, ""}, {31, 1, 31, "someday"}, {25, 0, 10, ""}} {
		if _, err := Due(d("2026-10-20"), tm, nil); err == nil {
			t.Errorf("%+v はエラー", tm)
		}
	}
}
