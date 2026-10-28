package health

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Delimiters are the separators of the CSV files: "," and ";" (spreadsheets of countries writing 72,5)
var Delimiters = []rune{',', ';', '\t', '|'}

// CSVFile is a CSV file read in memory: the health exports are small
type CSVFile struct {
	Delimiter rune
	HasHeader bool
	Header    []string   // the names of the columns, "Column 3" without header
	Rows      [][]string // without the header
	content   []byte
}

// ReadCSV reads a CSV file, guessing its delimiter and whether it has a header
func ReadCSV(path string) (*CSVFile, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content = bytes.TrimPrefix(content, []byte("\xef\xbb\xbf")) // the BOM of Excel
	f := &CSVFile{content: content}
	if err := f.Parse(sniffDelimiter(content), true); err != nil {
		return nil, err
	}
	// a first row with a date isn't a header
	if len(f.Header) > 0 {
		for _, cell := range f.Header {
			if _, ok := parseAnyDate(cell); ok {
				return f, f.Parse(f.Delimiter, false)
			}
		}
	}
	return f, nil
}

// sniffDelimiter returns the delimiter found the same number of times on the first lines
func sniffDelimiter(content []byte) rune {
	lines := strings.Split(strings.ReplaceAll(string(content[:min(len(content), 16*1024)]), "\r\n", "\n"), "\n")
	lines = lines[:min(len(lines), 10)]
	best, bestCount := ',', 0
	for _, d := range Delimiters {
		count, consistent := -1, true
		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}
			n := strings.Count(line, string(d))
			if count == -1 {
				count = n
			} else if n != count {
				consistent = false
			}
		}
		if consistent && count > bestCount {
			best, bestCount = d, count
		}
	}
	return best
}

// Parse reads the file again with a delimiter and a header chosen by the user
func (f *CSVFile) Parse(delimiter rune, header bool) error {
	r := csv.NewReader(bytes.NewReader(f.content))
	r.Comma = delimiter
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = true
	rows, err := r.ReadAll()
	if err != nil {
		return fmt.Errorf("not a CSV file: %w", err)
	}
	if len(rows) == 0 {
		return errors.New("the file is empty")
	}
	f.Delimiter, f.HasHeader, f.Rows = delimiter, header, rows
	columns := 0
	for _, row := range rows {
		columns = max(columns, len(row))
	}
	f.Header = make([]string, columns)
	for i := range f.Header {
		f.Header[i] = fmt.Sprintf("Column %d", i+1)
	}
	if header {
		for i, name := range rows[0] {
			if name = strings.TrimSpace(name); name != "" {
				f.Header[i] = name
			}
		}
		f.Rows = rows[1:]
	}
	return nil
}

// Column is a column of the file, -1 for none
type Column = int

const NoColumn Column = -1

// Mapping tells where the values of the measures are in the rows
type Mapping struct {
	Date       Column
	DateLayout string // a layout of DateLayouts
	Columns    map[Field]Column
	Pounds     bool // the masses (weight, bone) are in pounds
	// FromMass are the percentages given as masses (Withings: "Fat mass (kg)"), divided by the weight
	FromMass map[Field]bool
}

// Percentages are the fields that may be given as masses
var Percentages = []Field{Fat, Muscle, BodyWater}

// DateLayout is a format of dates, Layout for time.Parse, or unix times
type DateLayout struct {
	Label  string
	Layout string
}

const (
	unixSeconds      = "unix"
	unixMilliseconds = "unix-ms"
)

// DateLayouts are the formats of dates understood, day first before month first: a date valid in both is
// read day first
var DateLayouts = []DateLayout{
	{"2026-10-28 07:30:00", "2006-01-02 15:04:05"},
	{"2026-10-28T07:30:00+01:00", time.RFC3339},
	{"2026-10-28T07:30:00", "2006-01-02T15:04:05"},
	{"2026-10-28 07:30", "2006-01-02 15:04"},
	{"2026-10-28", "2006-01-02"},
	{"28/10/2026 07:30:00 (day first)", "02/01/2006 15:04:05"},
	{"28/10/2026 07:30 (day first)", "02/01/2006 15:04"},
	{"28/10/2026 (day first)", "02/01/2006"},
	{"10/28/2026 07:30:00 (month first)", "01/02/2006 15:04:05"},
	{"10/28/2026 07:30 (month first)", "01/02/2006 15:04"},
	{"10/28/2026 (month first)", "01/02/2006"},
	{"28.10.2026", "02.01.2006"},
	{"28.10.2026 07:30", "02.01.2006 15:04"},
	{"Unix time, seconds", unixSeconds},
	{"Unix time, milliseconds", unixMilliseconds},
}

// ParseDate reads a date of a layout, in the time zone of the computer when it has none
func ParseDate(layout, value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	switch layout {
	case unixSeconds, unixMilliseconds:
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n <= 0 {
			return time.Time{}, fmt.Errorf("%q is not a unix time", value)
		}
		if layout == unixMilliseconds {
			return time.UnixMilli(n), nil
		}
		return time.Unix(n, 0), nil
	}
	return time.ParseInLocation(layout, value, time.Local)
}

func parseAnyDate(value string) (string, bool) {
	for _, l := range DateLayouts {
		if l.Layout == unixSeconds || l.Layout == unixMilliseconds {
			continue // any number would be a date
		}
		if _, err := ParseDate(l.Layout, value); err == nil {
			return l.Layout, true
		}
	}
	return "", false
}

// DetectDateLayout returns the layout reading the most dates of a column, the first one of DateLayouts
// on a tie (day first). A layout reading less than half of them isn't the one: "" then. A few bad rows
// don't prevent it, they are told when reading the measures.
func (f *CSVFile) DetectDateLayout(column Column) string {
	if column < 0 {
		return ""
	}
	var values []string
	for _, row := range f.Rows {
		if column < len(row) && strings.TrimSpace(row[column]) != "" {
			values = append(values, row[column])
		}
	}
	best, bestCount := "", 0
	for _, l := range DateLayouts {
		count := 0
		for _, v := range values {
			// the numbers of a column of weights would be unix times of 1970
			if t, err := ParseDate(l.Layout, v); err == nil && t.Year() >= 1990 && t.Year() <= 2100 {
				count++
			}
		}
		if count > bestCount {
			best, bestCount = l.Layout, count
		}
	}
	if bestCount*2 <= len(values) {
		return ""
	}
	return best
}

// columnNames are words of the names of the columns, English and French, matched without case and
// accents. The order matters: "Fat mass" is fat, not weight.
var columnNames = []struct {
	field *Field // nil: the date
	words []string
}{
	{nil, []string{"date", "time", "timestamp", "jour", "heure"}},
	{ptr(Fat), []string{"fat", "graisse", "grasse", "adipeuse"}},
	{ptr(Muscle), []string{"muscle", "musculaire"}},
	{ptr(Bone), []string{"bone", "osseuse"}},
	{ptr(BodyWater), []string{"water", "hydration", "eau", "hydratation"}},
	{ptr(RestHR), []string{"resting", "rest hr", "repos", "rhr"}},
	{ptr(Vo2Max), []string{"vo2", "vo2max", "vo2 max"}},
	{ptr(Systolic), []string{"systolic", "systolique", "sys"}},
	{ptr(Diastolic), []string{"diastolic", "diastolique", "dia"}},
	{ptr(Weight), []string{"weight", "poids", "body mass", "masse corporelle"}},
}

func ptr(f Field) *Field { return &f }

var accents = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

func normalize(s string) string {
	s, _, _ = transform.String(accents, strings.ToLower(s))
	return s
}

// GuessMapping maps the columns by their names, and finds the format of the dates
func (f *CSVFile) GuessMapping() Mapping {
	m := Mapping{Date: NoColumn, Columns: map[Field]Column{}, FromMass: map[Field]bool{}}
	for _, field := range Fields {
		m.Columns[field] = NoColumn
	}
	taken := map[Column]bool{}
	for _, names := range columnNames {
		for i, name := range f.Header {
			if taken[i] || !f.HasHeader || !containsWord(normalize(name), names.words) {
				continue
			}
			taken[i] = true
			if names.field == nil {
				m.Date = i
			} else if m.Columns[*names.field] == NoColumn {
				m.Columns[*names.field] = i
				// "Fat mass (kg)": a mass, "Fat (%)" a percentage
				n := normalize(name)
				if slices.Contains(Percentages, *names.field) && !strings.Contains(n, "%") &&
					(strings.Contains(n, "kg") || strings.Contains(n, "mass")) {
					m.FromMass[*names.field] = true
				}
			}
			break
		}
	}
	if m.Date == NoColumn {
		// without header, or with an unknown name: the first column of dates
		for i := range f.Header {
			if f.DetectDateLayout(i) != "" {
				m.Date = i
				break
			}
		}
	}
	m.DateLayout = f.DetectDateLayout(m.Date)
	for _, name := range f.Header {
		if containsWord(normalize(name), []string{"lb", "lbs", "pound", "pounds"}) {
			m.Pounds = true
		}
	}
	return m
}

// containsWord tells whether name has one of the words, as a whole word: "sys" isn't in "physical"
func containsWord(name string, words []string) bool {
	for _, w := range words {
		for i := strings.Index(name, w); i >= 0; {
			end := i + len(w)
			before := i == 0 || !unicode.IsLetter(rune(name[i-1]))
			after := end == len(name) || !unicode.IsLetter(rune(name[end]))
			if before && after {
				return true
			}
			next := strings.Index(name[i+1:], w)
			if next < 0 {
				break
			}
			i += 1 + next
		}
	}
	return false
}

var number = regexp.MustCompile(`^[+-]?\d+(?:[.,]\d+)?`)

// parseNumber reads "72.5", "72,5", "72.5 kg"; "" or "-" is no value
func parseNumber(s string) (float64, bool, error) {
	s = strings.TrimSpace(strings.ReplaceAll(s, " ", ""))
	if s == "" || s == "-" {
		return 0, false, nil
	}
	found := number.FindString(strings.ReplaceAll(s, " ", ""))
	if found == "" {
		return 0, false, fmt.Errorf("%q is not a number", s)
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(found, ",", "."), 64)
	return v, err == nil, err
}

// RowError is a row of the file that gives no measure
type RowError struct {
	Line int // in the file, from 1
	Err  error
}

// CSVResult is what a mapping gives
type CSVResult struct {
	Measures []Measure // oldest first
	Lines    []int     // the line of each measure, for the errors of the server
	Errors   []RowError
	Empty    int // rows without any value
}

const poundKg = 0.45359237

// Measures reads the measures of the rows
func (f *CSVFile) Measures(m Mapping) (CSVResult, error) {
	var result CSVResult
	if m.Date == NoColumn {
		return result, errors.New("choose the column of the dates")
	}
	if m.DateLayout == "" {
		return result, errors.New("choose the format of the dates")
	}
	mapped := false
	for _, c := range m.Columns {
		mapped = mapped || c != NoColumn
	}
	if !mapped {
		return result, errors.New("choose at least one column of values")
	}
	if (m.Columns[Systolic] == NoColumn) != (m.Columns[Diastolic] == NoColumn) {
		return result, errors.New("the blood pressure needs both the systolic and the diastolic columns")
	}
	first := 1
	if f.HasHeader {
		first = 2
	}
	type indexed struct {
		Measure
		line int
	}
	var measures []indexed
	for i, row := range f.Rows {
		line := first + i
		if len(row) == 0 || (len(row) == 1 && strings.TrimSpace(row[0]) == "") {
			continue
		}
		measure, err := f.row(row, m)
		if err != nil {
			result.Errors = append(result.Errors, RowError{line, err})
			continue
		}
		if measure.Empty() {
			result.Empty++
			continue
		}
		measures = append(measures, indexed{measure, line})
	}
	// oldest first, the lines following
	sort.SliceStable(measures, func(i, j int) bool { return measures[i].Date.Before(measures[j].Date) })
	for _, x := range measures {
		result.Measures = append(result.Measures, x.Measure)
		result.Lines = append(result.Lines, x.line)
	}
	return result, nil
}

func (f *CSVFile) row(row []string, m Mapping) (Measure, error) {
	cell := func(c Column) string {
		if c < 0 || c >= len(row) {
			return ""
		}
		return row[c]
	}
	date, err := ParseDate(m.DateLayout, cell(m.Date))
	if err != nil {
		return Measure{}, fmt.Errorf("date %q: not in the chosen format", cell(m.Date))
	}
	measure := Measure{Date: date}
	values := map[Field]float64{}
	for _, field := range Fields {
		v, ok, err := parseNumber(cell(m.Columns[field]))
		if err != nil {
			return Measure{}, fmt.Errorf("%s: %w", field, err)
		}
		if ok {
			values[field] = v
		}
	}
	if m.Pounds {
		values[Weight] *= poundKg
		values[Bone] *= poundKg
	}
	for _, field := range Percentages {
		if !m.FromMass[field] || values[field] == 0 {
			continue
		}
		// a mass, in the unit of the weight: its share of the weight
		weight := values[Weight]
		if m.Pounds {
			weight /= poundKg
		}
		if weight == 0 {
			return Measure{}, fmt.Errorf("%s is a mass: the weight of the row is needed to make it a percentage", field)
		}
		values[field] = values[field] / weight * 100
	}
	for field, v := range values {
		measure.Set(field, v)
	}
	if (measure.Systolic == 0) != (measure.Diastolic == 0) {
		measure.Systolic, measure.Diastolic = 0, 0
	}
	return measure, nil
}
