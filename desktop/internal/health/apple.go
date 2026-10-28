package health

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// appleTypes are the types of Apple Health records MySportWeb stores
var appleTypes = map[string]Field{
	"HKQuantityTypeIdentifierBodyMass":               Weight,
	"HKQuantityTypeIdentifierBodyFatPercentage":      Fat,
	"HKQuantityTypeIdentifierRestingHeartRate":       RestHR,
	"HKQuantityTypeIdentifierVO2Max":                 Vo2Max,
	"HKQuantityTypeIdentifierBloodPressureSystolic":  Systolic,
	"HKQuantityTypeIdentifierBloodPressureDiastolic": Diastolic,
}

// appleDateLayout is the layout of the dates of export.xml, "2024-01-05 07:31:00 +0100"
const appleDateLayout = "2006-01-02 15:04:05 -0700"

type appleRecord struct {
	field  Field
	source string
	date   time.Time
	value  float64 // in the unit of MySportWeb
}

// AppleKind sums up the records of a value of MySportWeb found in an export
type AppleKind struct {
	Field       Field
	Count       int
	First, Last time.Time
}

// AppleOther counts the records of a type MySportWeb doesn't store
type AppleOther struct {
	Name  string // readable, "Step count"
	Count int
}

// AppleExport is what an Apple Health export holds for MySportWeb
type AppleExport struct {
	Kinds   []AppleKind  // the values MySportWeb stores, in the order of Fields
	Others  []AppleOther // the others, most frequent first
	Sources []string     // the applications and devices of the records of Kinds
	Skipped int          // records of Kinds MySportWeb can't read: unknown unit, invalid date
	records []appleRecord
}

// ReadAppleExport reads the export.zip of the Health application of an iPhone. progress receives the
// bytes of export.xml read and its size. export.xml is several GB for years of data: it is read line by
// line, only the records MySportWeb stores are decoded.
func ReadAppleExport(zipPath string, progress func(done, total int64)) (*AppleExport, error) {
	archive, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("not a zip file: %w", err)
	}
	defer archive.Close()
	file, err := findExportXML(&archive.Reader)
	if err != nil {
		return nil, err
	}
	r, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return readExportXML(&countingReader{r: r, total: int64(file.UncompressedSize64), progress: progress})
}

// findExportXML returns the export.xml of the archive, its name depends on the language of the iPhone
// (exportation.xml in French): the XML file whose root is HealthData
func findExportXML(archive *zip.Reader) (*zip.File, error) {
	for _, f := range archive.File {
		if !strings.EqualFold(path.Ext(f.Name), ".xml") || strings.Contains(strings.ToLower(f.Name), "cda") {
			continue
		}
		r, err := f.Open()
		if err != nil {
			continue
		}
		head := make([]byte, 64*1024)
		n, _ := io.ReadFull(r, head)
		r.Close()
		if bytes.Contains(head[:n], []byte("<HealthData")) {
			return f, nil
		}
	}
	return nil, errors.New("no export.xml in the archive: is it the export of the Health application?")
}

type countingReader struct {
	r        io.Reader
	done     int64
	total    int64
	progress func(done, total int64)
	reported int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.done += int64(n)
	if c.progress != nil && (c.done-c.reported > 4<<20 || err == io.EOF) {
		c.reported = c.done
		c.progress(c.done, c.total)
	}
	return n, err
}

var recordPrefix = []byte("<Record ")

func readExportXML(r io.Reader) (*AppleExport, error) {
	export := &AppleExport{}
	others := map[string]int{}
	// the records of a correlation (blood pressure) may be repeated. Not the record as key: time.Parse gives
	// each date its own location, two equal dates wouldn't be equal keys
	type key struct {
		field  Field
		source string
		unix   int64
		value  float64
	}
	seen := map[key]bool{}
	reader := bufio.NewReaderSize(r, 1<<20)
	var long []byte
	for {
		line, err := reader.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			long = append(long, line...)
			continue
		}
		if long != nil {
			line = append(long, line...)
			long = nil
		}
		if trimmed := bytes.TrimSpace(line); bytes.HasPrefix(trimmed, recordPrefix) {
			typ := attribute(trimmed, "type")
			if field, ok := appleTypes[typ]; ok {
				record, ok := parseAppleRecord(trimmed, field)
				k := key{record.field, record.source, record.date.Unix(), record.value}
				switch {
				case !ok:
					export.Skipped++
				case !seen[k]:
					seen[k] = true
					export.records = append(export.records, record)
				}
			} else if typ != "" {
				others[typ]++
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	if len(export.records) == 0 && len(others) == 0 {
		return nil, errors.New("no record in export.xml")
	}
	export.summarize(others)
	return export, nil
}

// attribute returns the value of an attribute without entities, as the type of a record: cheaper than
// decoding the XML of the millions of records MySportWeb doesn't store
func attribute(tag []byte, name string) string {
	key := []byte(" " + name + `="`)
	i := bytes.Index(tag, key)
	if i < 0 {
		return ""
	}
	rest := tag[i+len(key):]
	end := bytes.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	return string(rest[:end])
}

// parseAppleRecord decodes a record, its value converted to the unit of MySportWeb
func parseAppleRecord(tag []byte, field Field) (appleRecord, bool) {
	token, err := xml.NewDecoder(bytes.NewReader(tag)).Token()
	if err != nil {
		return appleRecord{}, false
	}
	start, ok := token.(xml.StartElement)
	if !ok {
		return appleRecord{}, false
	}
	attrs := map[string]string{}
	for _, a := range start.Attr {
		attrs[a.Name.Local] = a.Value
	}
	date, err := time.Parse(appleDateLayout, attrs["startDate"])
	if err != nil {
		return appleRecord{}, false
	}
	value, err := strconv.ParseFloat(attrs["value"], 64)
	if err != nil {
		return appleRecord{}, false
	}
	value, ok = appleValue(field, attrs["unit"], value)
	if !ok || value <= 0 {
		return appleRecord{}, false
	}
	return appleRecord{field: field, source: attrs["sourceName"], date: date, value: value}, true
}

// appleValue converts a value to the unit of MySportWeb
func appleValue(field Field, unit string, value float64) (float64, bool) {
	switch field {
	case Weight:
		factor, ok := map[string]float64{"kg": 1, "g": 0.001, "lb": 0.45359237, "st": 6.35029318}[unit]
		return value * factor, ok
	case Fat:
		// Health exports the percentages as fractions: 0.18 for 18 %
		if unit != "%" {
			return 0, false
		}
		if value <= 1 {
			value *= 100
		}
		return value, true
	case RestHR:
		return value, unit == "count/min"
	case Vo2Max:
		return value, strings.HasPrefix(unit, "mL/") || strings.HasPrefix(unit, "ml/")
	case Systolic, Diastolic:
		return value, unit == "mmHg"
	}
	return 0, false
}

func (e *AppleExport) summarize(others map[string]int) {
	kinds := map[Field]*AppleKind{}
	sources := map[string]bool{}
	for _, r := range e.records {
		k := kinds[r.field]
		if k == nil {
			k = &AppleKind{Field: r.field, First: r.date, Last: r.date}
			kinds[r.field] = k
		}
		k.Count++
		if r.date.Before(k.First) {
			k.First = r.date
		}
		if r.date.After(k.Last) {
			k.Last = r.date
		}
		sources[r.source] = true
	}
	for _, f := range Fields {
		if k, ok := kinds[f]; ok {
			e.Kinds = append(e.Kinds, *k)
		}
	}
	for s := range sources {
		e.Sources = append(e.Sources, s)
	}
	sort.Strings(e.Sources)
	for typ, count := range others {
		e.Others = append(e.Others, AppleOther{Name: readableType(typ), Count: count})
	}
	sort.Slice(e.Others, func(i, j int) bool {
		if e.Others[i].Count != e.Others[j].Count {
			return e.Others[i].Count > e.Others[j].Count
		}
		return e.Others[i].Name < e.Others[j].Name
	})
}

// readableType turns HKQuantityTypeIdentifierStepCount into "Step count"
func readableType(typ string) string {
	for _, prefix := range []string{"HKQuantityTypeIdentifier", "HKCategoryTypeIdentifier", "HKDataType"} {
		typ = strings.TrimPrefix(typ, prefix)
	}
	var b strings.Builder
	runes := []rune(typ)
	for i, r := range runes {
		startsWord := i+1 < len(runes) && unicode.IsLower(runes[i+1]) // "Count" in "StepCount", not "SDNN"
		if i > 0 && unicode.IsUpper(r) && (unicode.IsLower(runes[i-1]) || startsWord) {
			b.WriteRune(' ')
			if startsWord {
				r = unicode.ToLower(r)
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

// AppleSelection chooses the records to import
type AppleSelection struct {
	Fields  map[Field]bool
	Sources map[string]bool
	Since   time.Time // zero: all
}

// AppleResult is what a selection gives
type AppleResult struct {
	Measures []Measure // oldest first
	// UnpairedPressures are the systolic or diastolic pressures without the other one at the same date
	UnpairedPressures int
}

// Measures turns the selected records into measures: the values recorded at the same date (a smart scale
// gives the weight and the fat) make one measure, as the two values of a blood pressure
func (e *AppleExport) Measures(sel AppleSelection) AppleResult {
	byDate := map[int64]*Measure{}
	for _, r := range e.records {
		if !sel.Fields[r.field] || !sel.Sources[r.source] || r.date.Before(sel.Since) {
			continue
		}
		m := byDate[r.date.Unix()]
		if m == nil {
			m = &Measure{Date: r.date}
			byDate[r.date.Unix()] = m
		}
		// two sources at the same date: the first value is kept
		if m.Get(r.field) == 0 {
			m.Set(r.field, r.value)
		}
	}
	var result AppleResult
	for _, m := range byDate {
		if (m.Systolic == 0) != (m.Diastolic == 0) {
			result.UnpairedPressures++
			m.Systolic, m.Diastolic = 0, 0
		}
		if !m.Empty() {
			result.Measures = append(result.Measures, *m)
		}
	}
	sortByDate(result.Measures)
	return result
}
