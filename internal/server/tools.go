package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/corpnum"
	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/ctax"
	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/holiday"
	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/jpdate"
	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/payterm"
	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/stamp"
	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/textnorm"
	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/wareki"
	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/withholding"
)

func parseDate(field, s string) (jpdate.Date, error) {
	d, err := jpdate.Parse(s)
	if err != nil {
		return d, fmt.Errorf("%s: %w", field, err)
	}
	return d, nil
}

func yen(v int64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := fmt.Sprint(v)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String() + "円"
	}
	return b.String() + "円"
}

// ---- 祝日一覧 ----

type HolidaysListIn struct {
	Year int    `json:"year,omitempty" jsonschema:"年（西暦）。from/to を使わないときに指定。1949〜2150"`
	From string `json:"from,omitempty" jsonschema:"期間の始まり（YYYY-MM-DD）。year の代わりに使える"`
	To   string `json:"to,omitempty" jsonschema:"期間の終わり（YYYY-MM-DD）"`
}

type HolidayRow struct {
	Date      jpdate.Date  `json:"date"`
	Weekday   string       `json:"weekday"`
	Name      string       `json:"name"`
	Kind      holiday.Kind `json:"kind"`
	KindLabel string       `json:"kind_label"`
}

type HolidaysListOut struct {
	From       jpdate.Date  `json:"from"`
	To         jpdate.Date  `json:"to"`
	Count      int          `json:"count"`
	Holidays   []HolidayRow `json:"holidays"`
	IsEstimate bool         `json:"is_estimate"`
	Note       string       `json:"note"`
}

func (s *srv) holidaysList(_ context.Context, _ *mcp.CallToolRequest, in HolidaysListIn) (*mcp.CallToolResult, HolidaysListOut, error) {
	var from, to jpdate.Date
	var err error
	switch {
	case in.From != "" || in.To != "":
		if in.From == "" || in.To == "" {
			return nil, HolidaysListOut{}, fmt.Errorf("from と to は両方指定してください")
		}
		if from, err = parseDate("from", in.From); err != nil {
			return nil, HolidaysListOut{}, err
		}
		if to, err = parseDate("to", in.To); err != nil {
			return nil, HolidaysListOut{}, err
		}
	case in.Year != 0:
		if in.Year < holiday.MinYear || in.Year > holiday.MaxYear {
			return nil, HolidaysListOut{}, holiday.ErrOutOfRange{Year: in.Year}
		}
		from = jpdate.MustNew(in.Year, time.January, 1)
		to = jpdate.MustNew(in.Year, time.December, 31)
	default:
		y := s.today().Year()
		from = jpdate.MustNew(y, time.January, 1)
		to = jpdate.MustNew(y, time.December, 31)
	}
	if to.Before(from) {
		return nil, HolidaysListOut{}, fmt.Errorf("to（%s）が from（%s）より前です", to, from)
	}
	if to.Year()-from.Year() > 10 {
		return nil, HolidaysListOut{}, fmt.Errorf("期間は10年以内で指定してください")
	}
	out := HolidaysListOut{From: from, To: to, Holidays: []HolidayRow{}}
	for y := from.Year(); y <= to.Year(); y++ {
		hs, err := holiday.Year(y)
		if err != nil {
			return nil, HolidaysListOut{}, err
		}
		for _, h := range hs {
			if h.Date.Before(from) || h.Date.After(to) {
				continue
			}
			out.Holidays = append(out.Holidays, HolidayRow{h.Date, jpdate.WeekdayJA(h.Date.Weekday()), h.Name, h.Kind, holiday.KindLabel(h.Kind)})
		}
		if holiday.IsEstimate(y) {
			out.IsEstimate = true
		}
	}
	out.Count = len(out.Holidays)
	out.Note = "国民の祝日に関する法律と特例法の規則から計算（1955〜2027年は内閣府の公表データと全日一致を確認済み）"
	if out.IsEstimate {
		out.Note += fmt.Sprintf("。%d年より後の春分の日・秋分の日は、前年2月の官報公表で確定する日付を計算式で求めた見込み", holiday.LastGazettedYear)
	}
	return nil, out, nil
}

// ---- 営業日 ----

type BusinessDayCheckIn struct {
	Date     string          `json:"date" jsonschema:"確かめる日（YYYY-MM-DD）"`
	Calendar CalendarOptions `json:"calendar,omitempty" jsonschema:"会社ごとの休み・営業日の設定"`
}

type BusinessDayCheckOut struct {
	holiday.DayStatus
	PrevBusinessDay jpdate.Date `json:"previous_business_day"`
	NextBusinessDay jpdate.Date `json:"next_business_day"`
	Summary         string      `json:"summary"`
}

func (s *srv) businessDayCheck(_ context.Context, _ *mcp.CallToolRequest, in BusinessDayCheckIn) (*mcp.CallToolResult, BusinessDayCheckOut, error) {
	d, err := parseDate("date", in.Date)
	if err != nil {
		return nil, BusinessDayCheckOut{}, err
	}
	cal, err := in.Calendar.build(s.cfg.DefaultCalendar)
	if err != nil {
		return nil, BusinessDayCheckOut{}, err
	}
	st, err := cal.Check(d)
	if err != nil {
		return nil, BusinessDayCheckOut{}, err
	}
	out := BusinessDayCheckOut{DayStatus: st}
	if out.PrevBusinessDay, err = cal.AddBusinessDays(d, -1); err != nil {
		return nil, BusinessDayCheckOut{}, err
	}
	if out.NextBusinessDay, err = cal.AddBusinessDays(d, 1); err != nil {
		return nil, BusinessDayCheckOut{}, err
	}
	if st.Business {
		out.Summary = fmt.Sprintf("%s（%s）は営業日です", d, st.Weekday)
	} else {
		out.Summary = fmt.Sprintf("%s（%s）は休み：%s。次の営業日は %s", d, st.Weekday, st.Reason, out.NextBusinessDay)
	}
	return nil, out, nil
}

type BusinessDayAddIn struct {
	Date     string          `json:"date" jsonschema:"起点の日（YYYY-MM-DD）。起点の日は数えない"`
	Days     int             `json:"days" jsonschema:"進める営業日数。負なら前へ。0 は起点が休みなら次の営業日"`
	Calendar CalendarOptions `json:"calendar,omitempty" jsonschema:"会社ごとの休み・営業日の設定"`
}

type SkippedDay struct {
	Date   jpdate.Date `json:"date"`
	Reason string      `json:"reason"`
}

type BusinessDayAddOut struct {
	From         jpdate.Date  `json:"from"`
	Days         int          `json:"days"`
	Result       jpdate.Date  `json:"result"`
	Weekday      string       `json:"weekday"`
	CalendarDays int          `json:"calendar_days"`
	Skipped      []SkippedDay `json:"skipped_days_off"`
	Summary      string       `json:"summary"`
}

func (s *srv) businessDayAdd(_ context.Context, _ *mcp.CallToolRequest, in BusinessDayAddIn) (*mcp.CallToolResult, BusinessDayAddOut, error) {
	d, err := parseDate("date", in.Date)
	if err != nil {
		return nil, BusinessDayAddOut{}, err
	}
	cal, err := in.Calendar.build(s.cfg.DefaultCalendar)
	if err != nil {
		return nil, BusinessDayAddOut{}, err
	}
	r, err := cal.AddBusinessDays(d, in.Days)
	if err != nil {
		return nil, BusinessDayAddOut{}, err
	}
	out := BusinessDayAddOut{From: d, Days: in.Days, Result: r, Weekday: jpdate.WeekdayJA(r.Weekday()), CalendarDays: d.DaysUntil(r), Skipped: []SkippedDay{}}
	lo, hi := d, r
	if hi.Before(lo) {
		lo, hi = hi, lo
	}
	if lo.DaysUntil(hi) <= 400 {
		for x := lo; !x.After(hi); x = x.AddDays(1) {
			if x.Equal(d) && in.Days != 0 {
				continue
			}
			st, err := cal.Check(x)
			if err != nil {
				return nil, BusinessDayAddOut{}, err
			}
			if !st.Business {
				out.Skipped = append(out.Skipped, SkippedDay{x, st.Reason})
			}
		}
	}
	dir := "後"
	if in.Days < 0 {
		dir = "前"
	}
	n := in.Days
	if n < 0 {
		n = -n
	}
	out.Summary = fmt.Sprintf("%s の %d営業日%s は %s（%s）", d, n, dir, r, out.Weekday)
	return nil, out, nil
}

type BusinessDayCountIn struct {
	From     string          `json:"from" jsonschema:"始まりの日（YYYY-MM-DD・この日を含む）"`
	To       string          `json:"to" jsonschema:"終わりの日（YYYY-MM-DD・この日を含む）"`
	Calendar CalendarOptions `json:"calendar,omitempty" jsonschema:"会社ごとの休み・営業日の設定"`
}

type BusinessDayCountOut struct {
	holiday.CountResult
	Summary string `json:"summary"`
}

func (s *srv) businessDayCount(_ context.Context, _ *mcp.CallToolRequest, in BusinessDayCountIn) (*mcp.CallToolResult, BusinessDayCountOut, error) {
	from, err := parseDate("from", in.From)
	if err != nil {
		return nil, BusinessDayCountOut{}, err
	}
	to, err := parseDate("to", in.To)
	if err != nil {
		return nil, BusinessDayCountOut{}, err
	}
	cal, err := in.Calendar.build(s.cfg.DefaultCalendar)
	if err != nil {
		return nil, BusinessDayCountOut{}, err
	}
	r, err := cal.Count(from, to)
	if err != nil {
		return nil, BusinessDayCountOut{}, err
	}
	return nil, BusinessDayCountOut{r, fmt.Sprintf("%s〜%s の %d日間のうち、営業日は %d日", from, to, r.CalendarDays, r.BusinessDays)}, nil
}

// ---- 登録番号・法人番号 ----

type NumberValidateIn struct {
	Numbers []string `json:"numbers" jsonschema:"確かめる番号（1〜1000件）。T で始まれば適格請求書発行事業者の登録番号、13桁の数字だけなら法人番号。全角・ハイフン・空白は読み飛ばす"`
}

type NumberRow struct {
	corpnum.Result
	FormatError string `json:"format_error,omitempty"`
}

type NumberValidateOut struct {
	Results     []NumberRow `json:"results"`
	Valid       int         `json:"valid"`
	Invalid     int         `json:"invalid"`
	FormatError int         `json:"format_errors"`
	Summary     string      `json:"summary"`
}

func (s *srv) numberValidate(_ context.Context, _ *mcp.CallToolRequest, in NumberValidateIn) (*mcp.CallToolResult, NumberValidateOut, error) {
	if len(in.Numbers) == 0 || len(in.Numbers) > 1000 {
		return nil, NumberValidateOut{}, fmt.Errorf("numbers は1〜1000件で指定してください（%d件）", len(in.Numbers))
	}
	out := NumberValidateOut{Results: []NumberRow{}}
	for _, n := range in.Numbers {
		r, err := corpnum.Validate(n)
		row := NumberRow{Result: r}
		switch {
		case err != nil:
			row.FormatError = err.Error()
			row.Message = err.Error()
			out.FormatError++
		case r.Valid:
			out.Valid++
		default:
			out.Invalid++
		}
		out.Results = append(out.Results, row)
	}
	out.Summary = fmt.Sprintf("%d件中 正しい %d件・検査用数字の不一致 %d件・形式の誤り %d件（実在と登録の有無は国税庁の公表サイトで確認できます）", len(in.Numbers), out.Valid, out.Invalid, out.FormatError)
	return nil, out, nil
}

// ---- 消費税 ----

type TaxLine struct {
	Description string `json:"description,omitempty" jsonschema:"品名"`
	Amount      int64  `json:"amount,omitempty" jsonschema:"その行の金額（円）。unit_price×quantity の代わり。値引きは負の数"`
	UnitPrice   int64  `json:"unit_price,omitempty" jsonschema:"単価（円）"`
	Quantity    int64  `json:"quantity,omitempty" jsonschema:"数量"`
	Rate        int    `json:"rate" jsonschema:"税率（10・8・0）。0 は非課税・対象外"`
}

type ConsumptionTaxIn struct {
	Lines     []TaxLine `json:"lines" jsonschema:"明細"`
	PriceMode string    `json:"price_mode,omitempty" jsonschema:"金額が税抜なら exclusive（既定）、税込なら inclusive"`
	Rounding  string    `json:"rounding,omitempty" jsonschema:"端数処理。floor（切り捨て・既定）・round（四捨五入）・ceil（切り上げ）"`
}

type ConsumptionTaxOut struct {
	ctax.Result
	Summary string `json:"summary"`
}

func (s *srv) consumptionTax(_ context.Context, _ *mcp.CallToolRequest, in ConsumptionTaxIn) (*mcp.CallToolResult, ConsumptionTaxOut, error) {
	mode, err := ctax.ParseMode(in.PriceMode)
	if err != nil {
		return nil, ConsumptionTaxOut{}, err
	}
	r, err := ctax.ParseRounding(in.Rounding)
	if err != nil {
		return nil, ConsumptionTaxOut{}, err
	}
	lines := make([]ctax.Line, len(in.Lines))
	for i, l := range in.Lines {
		lines[i] = ctax.Line(l)
	}
	res, err := ctax.Calculate(lines, mode, r)
	if err != nil {
		return nil, ConsumptionTaxOut{}, err
	}
	parts := []string{}
	for _, b := range res.ByRate {
		if b.Rate > 0 {
			parts = append(parts, fmt.Sprintf("%d%%対象 %s（税 %s）", b.Rate, yen(b.TaxExcluded), yen(b.Tax)))
		}
	}
	sum := fmt.Sprintf("税込合計 %s・消費税 %s", yen(res.TotalIncluded), yen(res.TotalTax))
	if len(parts) > 0 {
		sum += "：" + strings.Join(parts, "、")
	}
	return nil, ConsumptionTaxOut{res, sum}, nil
}

type InvoiceItemIn struct {
	Description string `json:"description" jsonschema:"品名・取引内容"`
	Amount      int64  `json:"amount" jsonschema:"金額（円）"`
	Rate        int    `json:"rate" jsonschema:"税率（10・8・0）"`
	ReducedMark bool   `json:"reduced_mark,omitempty" jsonschema:"軽減税率の対象品目である旨（※印と注記など）が書かれているか"`
}

type InvoiceRateTotalIn struct {
	Rate  int    `json:"rate" jsonschema:"税率（10・8）"`
	Total *int64 `json:"total,omitempty" jsonschema:"請求書に書かれた、その税率の対価の合計（price_mode に合わせて税抜または税込）"`
	Tax   *int64 `json:"tax,omitempty" jsonschema:"請求書に書かれた、その税率の消費税額"`
}

type InvoiceCheckIn struct {
	Kind               string               `json:"kind,omitempty" jsonschema:"standard（適格請求書・既定）か simplified（適格簡易請求書：小売・飲食・タクシーなど）"`
	IssuerName         string               `json:"issuer_name,omitempty" jsonschema:"発行事業者の氏名又は名称"`
	RegistrationNumber string               `json:"registration_number,omitempty" jsonschema:"登録番号（T＋13桁）"`
	TransactionDate    string               `json:"transaction_date,omitempty" jsonschema:"取引年月日（YYYY-MM-DD。月まとめなら 2026年10月分 など）"`
	RecipientName      string               `json:"recipient_name,omitempty" jsonschema:"交付を受ける事業者の氏名又は名称（宛名）"`
	PriceMode          string               `json:"price_mode,omitempty" jsonschema:"金額が税抜なら exclusive（既定）、税込なら inclusive"`
	Items              []InvoiceItemIn      `json:"items,omitempty" jsonschema:"明細"`
	RateTotals         []InvoiceRateTotalIn `json:"rate_totals,omitempty" jsonschema:"請求書に書かれた税率ごとの合計と税額。この行があれば適用税率が書かれているとみなす"`
}

func (s *srv) invoiceCheck(_ context.Context, _ *mcp.CallToolRequest, in InvoiceCheckIn) (*mcp.CallToolResult, ctax.CheckResult, error) {
	inv := ctax.Invoice{
		Kind: ctax.InvoiceKind(in.Kind), IssuerName: in.IssuerName, RegistrationNumber: in.RegistrationNumber,
		TransactionDate: in.TransactionDate, RecipientName: in.RecipientName, PriceMode: in.PriceMode,
	}
	for _, it := range in.Items {
		inv.Items = append(inv.Items, ctax.InvoiceItem(it))
	}
	for _, rt := range in.RateTotals {
		inv.RateTotals = append(inv.RateTotals, ctax.InvoiceRateTotal(rt))
	}
	res, err := ctax.CheckInvoice(inv)
	return nil, res, err
}

// ---- 源泉徴収 ----

type WithholdingIn struct {
	Amount         int64  `json:"amount,omitempty" jsonschema:"報酬額（円）。請求書で消費税を区分しているなら税抜の額"`
	ConsumptionTax int64  `json:"consumption_tax,omitempty" jsonschema:"請求書で区分された消費税額（支払額の計算に使う）"`
	Category       string `json:"category,omitempty" jsonschema:"general（原稿料・デザイン料・講演料・士業など・既定）か judicial_scrivener（司法書士・土地家屋調査士・海事代理士）"`
	PaymentDate    string `json:"payment_date,omitempty" jsonschema:"支払日（YYYY-MM-DD）。2038年以降は復興特別所得税が無くなるため結果が変わる。省略時は今日"`
	NetTarget      int64  `json:"net_target,omitempty" jsonschema:"手取り（源泉徴収後・消費税を除く）をこの額にしたいときの報酬額を逆算する"`
}

type WithholdingOut struct {
	withholding.Result
	Reverse *ReverseResult `json:"reverse,omitempty"`
	Summary string         `json:"summary"`
}

type ReverseResult struct {
	NetTarget int64  `json:"net_target"`
	Exact     bool   `json:"exact"`
	Gross     int64  `json:"gross"`
	GrossNext int64  `json:"gross_next,omitempty"`
	Note      string `json:"note"`
}

func (s *srv) withholdingTax(_ context.Context, _ *mcp.CallToolRequest, in WithholdingIn) (*mcp.CallToolResult, WithholdingOut, error) {
	var date jpdate.Date
	if in.PaymentDate != "" {
		var err error
		if date, err = parseDate("payment_date", in.PaymentDate); err != nil {
			return nil, WithholdingOut{}, err
		}
	} else {
		date = s.today()
	}
	cat := withholding.Category(in.Category)
	amount := in.Amount
	var rev *ReverseResult
	if in.NetTarget > 0 {
		exact, lo, hi, err := withholding.GrossForNet(in.NetTarget, cat, date)
		if err != nil {
			return nil, WithholdingOut{}, err
		}
		rev = &ReverseResult{NetTarget: in.NetTarget, Exact: exact, Gross: lo}
		if exact {
			rev.Note = fmt.Sprintf("報酬額 %s で手取りがちょうど %s になります", yen(lo), yen(in.NetTarget))
			amount = lo
		} else {
			rev.GrossNext = hi
			rev.Note = fmt.Sprintf("手取りがちょうど %s になる報酬額はありません（%s で手前、%s で超える）", yen(in.NetTarget), yen(lo), yen(hi))
			amount = hi
		}
	} else if in.Amount <= 0 {
		return nil, WithholdingOut{}, fmt.Errorf("amount（報酬額）か net_target（手取り）を指定してください")
	}
	r, err := withholding.Calculate(withholding.Input{Amount: amount, ConsumptionTax: in.ConsumptionTax, Category: cat, PaymentDate: date})
	if err != nil {
		return nil, WithholdingOut{}, err
	}
	sum := fmt.Sprintf("報酬 %s の源泉徴収税額は %s、支払額は %s", yen(r.Amount), yen(r.Withholding), yen(r.NetPayment))
	return nil, WithholdingOut{Result: r, Reverse: rev, Summary: sum}, nil
}

// ---- 和暦 ----

type WarekiIn struct {
	Input string `json:"input" jsonschema:"変換する日付・年。例：2026-10-20、2026年、令和8年10月20日、R8.10.20、令和元年、平成三十一年四月三十日"`
}

type WarekiOut struct {
	Results []wareki.Result `json:"results"`
	Summary string          `json:"summary"`
}

func (s *srv) warekiConvert(_ context.Context, _ *mcp.CallToolRequest, in WarekiIn) (*mcp.CallToolResult, WarekiOut, error) {
	rs, err := wareki.Convert(in.Input)
	if err != nil {
		return nil, WarekiOut{}, err
	}
	parts := make([]string, len(rs))
	for i, r := range rs {
		parts[i] = r.Wareki + " = " + r.Gregorian
	}
	return nil, WarekiOut{rs, strings.Join(parts, "／")}, nil
}

// ---- 表記ゆれ ----

type NormalizeIn struct {
	Text string `json:"text" jsonschema:"正規化する文字列（10万バイトまで）"`
	Mode string `json:"mode,omitempty" jsonschema:"general（既定）か address（住所：丁目・番地・号をハイフンに、郵便番号を整える）"`
	Kana string `json:"kana,omitempty" jsonschema:"keep（既定）・katakana（ひらがなをカタカナに）・hiragana（カタカナをひらがなに）"`
}

func (s *srv) normalize(_ context.Context, _ *mcp.CallToolRequest, in NormalizeIn) (*mcp.CallToolResult, textnorm.Result, error) {
	if len(in.Text) > textnorm.MaxInput {
		return nil, textnorm.Result{}, fmt.Errorf("text は10万バイトまでです（%dバイト）", len(in.Text))
	}
	o := textnorm.Options{Mode: textnorm.General, Kana: textnorm.KeepKana}
	switch in.Mode {
	case "", "general":
	case "address":
		o.Mode = textnorm.Address
	default:
		return nil, textnorm.Result{}, fmt.Errorf("mode %q は general か address です", in.Mode)
	}
	switch in.Kana {
	case "", "keep":
	case "katakana":
		o.Kana = textnorm.Katakana
	case "hiragana":
		o.Kana = textnorm.Hiragana
	default:
		return nil, textnorm.Result{}, fmt.Errorf("kana %q は keep・katakana・hiragana のどれかです", in.Kana)
	}
	return nil, textnorm.Normalize(in.Text, o), nil
}

// ---- 支払期日 ----

type PaymentDueIn struct {
	TransactionDate string          `json:"transaction_date" jsonschema:"取引日（納品日・検収日など。YYYY-MM-DD）"`
	ClosingDay      int             `json:"closing_day" jsonschema:"締め日（1〜28、末日は 31）"`
	MonthOffset     int             `json:"month_offset" jsonschema:"締めた月から何か月後に払うか（0 は当月、1 は翌月、2 は翌々月）"`
	PayDay          int             `json:"pay_day" jsonschema:"支払日（1〜28、末日は 31）"`
	HolidayAdjust   string          `json:"holiday_adjust,omitempty" jsonschema:"支払日が休みのとき：previous（前営業日・既定）・next（翌営業日）・none（そのまま）"`
	Calendar        CalendarOptions `json:"calendar,omitempty" jsonschema:"会社ごとの休み・営業日の設定"`
}

type PaymentDueOut struct {
	payterm.Result
	Summary string `json:"summary"`
}

func (s *srv) paymentDue(_ context.Context, _ *mcp.CallToolRequest, in PaymentDueIn) (*mcp.CallToolResult, PaymentDueOut, error) {
	d, err := parseDate("transaction_date", in.TransactionDate)
	if err != nil {
		return nil, PaymentDueOut{}, err
	}
	cal, err := in.Calendar.build(s.cfg.DefaultCalendar)
	if err != nil {
		return nil, PaymentDueOut{}, err
	}
	r, err := payterm.Due(d, payterm.Terms{ClosingDay: in.ClosingDay, MonthOffset: in.MonthOffset, PayDay: in.PayDay, Adjust: in.HolidayAdjust}, cal)
	if err != nil {
		return nil, PaymentDueOut{}, err
	}
	sum := fmt.Sprintf("%s：%s の取引は %s 締め、支払日は %s（%s）", r.TermsLabel, d, r.ClosingDate, r.PayDate, r.PayWeekday)
	if r.Adjusted {
		sum += "。" + r.AdjustReason
	}
	return nil, PaymentDueOut{r, sum}, nil
}

// ---- 印紙税 ----

type StampIn struct {
	Amount         int64 `json:"amount,omitempty" jsonschema:"領収書の受取金額（円）"`
	ConsumptionTax int64 `json:"consumption_tax,omitempty" jsonschema:"領収書に区分して書く消費税額等（円）"`
	TaxStated      bool  `json:"tax_stated,omitempty" jsonschema:"消費税額等を区分して書くなら true（税抜の金額で判定される）"`
	AmountUnstated bool  `json:"amount_unstated,omitempty" jsonschema:"金額を書かない領収書なら true"`
	OtherThanSales bool  `json:"other_than_sales,omitempty" jsonschema:"売上代金以外の受取り（借入金・保証金・損害賠償金など）なら true"`
	Electronic     bool  `json:"electronic,omitempty" jsonschema:"PDF・メールなど電子データだけで渡すなら true"`
	NonBusiness    bool  `json:"non_business,omitempty" jsonschema:"営業に関しない受取り（個人が私物を売った代金など）なら true"`
}

type StampOut struct {
	stamp.Result
	Summary string `json:"summary"`
}

func (s *srv) revenueStamp(_ context.Context, _ *mcp.CallToolRequest, in StampIn) (*mcp.CallToolResult, StampOut, error) {
	if in.ConsumptionTax > 0 && !in.TaxStated {
		in.TaxStated = true
	}
	r, err := stamp.Calculate(stamp.Input{
		Amount: in.Amount, TaxStated: in.TaxStated, ConsumptionTax: in.ConsumptionTax, AmountUnstated: in.AmountUnstated,
		SalesProceeds: !in.OtherThanSales, Electronic: in.Electronic, NonBusiness: in.NonBusiness,
	})
	if err != nil {
		return nil, StampOut{}, err
	}
	sum := "収入印紙は不要です（" + r.Reason + "）"
	if r.Stamp > 0 {
		sum = fmt.Sprintf("収入印紙は %s です（%s）", yen(r.Stamp), r.Bracket)
	}
	return nil, StampOut{r, sum}, nil
}
