package health

import (
	"archive/zip"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// exportXML looks like an export of the Health application: a DTD, records on their own lines, a blood
// pressure correlation repeating its records
const exportXML = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE HealthData [
<!ELEMENT HealthData (ExportDate,Me,(Record|Correlation|Workout|ActivitySummary)*)>
<!ATTLIST Record
  type          CDATA #REQUIRED
  value         CDATA #IMPLIED
>
]>
<HealthData locale="fr_FR">
 <ExportDate value="2026-10-28 08:00:00 +0100"/>
 <Me HKCharacteristicTypeIdentifierBiologicalSex="HKBiologicalSexMale"/>
 <Record type="HKQuantityTypeIdentifierStepCount" sourceName="iPhone" unit="count" startDate="2026-10-01 08:00:00 +0200" endDate="2026-10-01 08:10:00 +0200" value="812"/>
 <Record type="HKQuantityTypeIdentifierStepCount" sourceName="iPhone" unit="count" startDate="2026-10-01 09:00:00 +0200" endDate="2026-10-01 09:10:00 +0200" value="300"/>
 <Record type="HKQuantityTypeIdentifierBodyMass" sourceName="Withings" unit="kg" startDate="2026-10-01 07:30:00 +0200" endDate="2026-10-01 07:30:00 +0200" value="72.4">
  <MetadataEntry key="HKWasUserEntered" value="0"/>
 </Record>
 <Record type="HKQuantityTypeIdentifierBodyFatPercentage" sourceName="Withings" unit="%" startDate="2026-10-01 07:30:00 +0200" endDate="2026-10-01 07:30:00 +0200" value="0.183"/>
 <Record type="HKQuantityTypeIdentifierBodyMass" sourceName="Santé &amp; moi" unit="lb" startDate="2026-10-02 07:30:00 +0200" endDate="2026-10-02 07:30:00 +0200" value="160"/>
 <Record type="HKQuantityTypeIdentifierRestingHeartRate" sourceName="Apple Watch" unit="count/min" startDate="2026-10-02 00:00:00 +0200" endDate="2026-10-02 23:59:59 +0200" value="48"/>
 <Record type="HKQuantityTypeIdentifierVO2Max" sourceName="Apple Watch" unit="mL/min·kg" startDate="2026-10-03 18:00:00 +0200" endDate="2026-10-03 18:00:00 +0200" value="51.6"/>
 <Record type="HKQuantityTypeIdentifierBodyMass" sourceName="Withings" unit="furlong" startDate="2026-10-04 07:30:00 +0200" endDate="2026-10-04 07:30:00 +0200" value="1"/>
 <Record type="HKQuantityTypeIdentifierBloodPressureDiastolic" sourceName="Withings" unit="mmHg" startDate="2026-10-05 08:00:00 +0200" endDate="2026-10-05 08:00:00 +0200" value="78"/>
 <Correlation type="HKCorrelationTypeIdentifierBloodPressure" sourceName="Withings" startDate="2026-10-05 08:00:00 +0200" endDate="2026-10-05 08:00:00 +0200">
  <Record type="HKQuantityTypeIdentifierBloodPressureDiastolic" sourceName="Withings" unit="mmHg" startDate="2026-10-05 08:00:00 +0200" endDate="2026-10-05 08:00:00 +0200" value="78"/>
  <Record type="HKQuantityTypeIdentifierBloodPressureSystolic" sourceName="Withings" unit="mmHg" startDate="2026-10-05 08:00:00 +0200" endDate="2026-10-05 08:00:00 +0200" value="121"/>
 </Correlation>
 <Record type="HKQuantityTypeIdentifierBloodPressureSystolic" sourceName="Withings" unit="mmHg" startDate="2026-10-06 08:00:00 +0200" endDate="2026-10-06 08:00:00 +0200" value="125"/>
 <Record type="HKCategoryTypeIdentifierSleepAnalysis" sourceName="Apple Watch" startDate="2026-10-01 23:00:00 +0200" endDate="2026-10-02 07:00:00 +0200" value="HKCategoryValueSleepAnalysisAsleepCore"/>
 <Workout workoutActivityType="HKWorkoutActivityTypeRunning" duration="30"/>
</HealthData>
`

func writeZip(t *testing.T, files map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "export.zip")
	out, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(out)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte(content))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out.Close()
	return p
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func TestReadAppleExport(t *testing.T) {
	p := writeZip(t, map[string]string{
		"apple_health_export/export_cda.xml":                      "<ClinicalDocument/>",
		"apple_health_export/exportation.xml":                     exportXML, // a French iPhone
		"apple_health_export/workout-routes/route_2026-10-01.gpx": "<gpx/>",
	})
	var progressed bool
	export, err := ReadAppleExport(p, func(done, total int64) { progressed = done == total })
	if err != nil {
		t.Fatal(err)
	}
	if !progressed {
		t.Error("the progress didn't reach the end")
	}

	kinds := map[Field]int{}
	for _, k := range export.Kinds {
		kinds[k.Field] = k.Count
	}
	// the repeated diastolic pressure once, the weight in furlongs skipped
	want := map[Field]int{Weight: 2, Fat: 1, RestHR: 1, Vo2Max: 1, Systolic: 2, Diastolic: 1}
	for f, n := range want {
		if kinds[f] != n {
			t.Errorf("%s: %d records, want %d", f, kinds[f], n)
		}
	}
	if export.Skipped != 1 {
		t.Errorf("%d skipped, want the weight in furlongs", export.Skipped)
	}
	if len(export.Others) != 2 || export.Others[0].Name != "Step count" || export.Others[0].Count != 2 ||
		export.Others[1].Name != "Sleep analysis" {
		t.Errorf("others: %+v", export.Others)
	}
	if strings.Join(export.Sources, ",") != "Apple Watch,Santé & moi,Withings" {
		t.Errorf("sources: %v", export.Sources)
	}

	all := AppleSelection{Fields: map[Field]bool{}, Sources: map[string]bool{}}
	for _, f := range Fields {
		all.Fields[f] = true
	}
	for _, s := range export.Sources {
		all.Sources[s] = true
	}
	result := export.Measures(all)
	// 1st: weight + fat of the scale; 2nd: weight in pounds, resting heart rate at another time; 3rd: VO2max;
	// 5th: the blood pressure; the systolic alone of the 6th is dropped
	if len(result.Measures) != 5 || result.UnpairedPressures != 1 {
		t.Fatalf("%d measures, %d unpaired: %+v", len(result.Measures), result.UnpairedPressures, result.Measures)
	}
	scale := result.Measures[0]
	if !near(scale.Weight, 72.4) || !near(scale.Fat, 18.3) || scale.Date.Day() != 1 {
		t.Errorf("scale: %+v", scale)
	}
	if rest := result.Measures[1]; rest.RestHR != 48 {
		t.Errorf("resting heart rate: %+v", rest)
	}
	if pounds := result.Measures[2]; !near(pounds.Weight, 72.57) {
		t.Errorf("160 lb: %+v", pounds)
	}
	if vo2 := result.Measures[3]; vo2.Vo2Max != 52 {
		t.Errorf("VO2max: %+v", vo2)
	}
	if bp := result.Measures[4]; bp.Systolic != 121 || bp.Diastolic != 78 {
		t.Errorf("blood pressure: %+v", bp)
	}

	// only the weight of Withings, since October 2
	sel := AppleSelection{Fields: map[Field]bool{Weight: true}, Sources: map[string]bool{"Withings": true},
		Since: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)}
	if r := export.Measures(sel); len(r.Measures) != 0 {
		t.Errorf("filtered: %+v", r.Measures)
	}
	sel.Since = time.Time{}
	if r := export.Measures(sel); len(r.Measures) != 1 || r.Measures[0].Fat != 0 {
		t.Errorf("weight only: %+v", r.Measures)
	}
}

func TestReadAppleExportRefusesOtherZip(t *testing.T) {
	p := writeZip(t, map[string]string{"photos/readme.xml": "<photos/>"})
	if _, err := ReadAppleExport(p, nil); err == nil {
		t.Error("a zip without export.xml was accepted")
	}
}

func TestReadableType(t *testing.T) {
	for typ, want := range map[string]string{
		"HKQuantityTypeIdentifierStepCount":                "Step count",
		"HKQuantityTypeIdentifierVO2Max":                   "VO2 max",
		"HKCategoryTypeIdentifierSleepAnalysis":            "Sleep analysis",
		"HKQuantityTypeIdentifierHeartRateVariabilitySDNN": "Heart rate variability SDNN",
	} {
		if got := readableType(typ); got != want {
			t.Errorf("%s: %q, want %q", typ, got, want)
		}
	}
}

func readCSV(t *testing.T, content string) *CSVFile {
	t.Helper()
	p := filepath.Join(t.TempDir(), "data.csv")
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	f, err := ReadCSV(p)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestCSVWithings(t *testing.T) {
	// the weight.csv of a Withings export: masses in kg
	f := readCSV(t, "\xef\xbb\xbf"+`Date,"Weight (kg)","Fat mass (kg)","Bone mass (kg)","Muscle mass (kg)","Hydration (kg)",Comments
"2026-10-02 07:31:00",72.0,14.4,3.1,57.6,39.6,
"2026-10-01 07:30:00",72.5,,,,,"after holidays"
"2026-10-03 07:30:00",,,,,,
"oops",71,,,,,
`)
	if f.Delimiter != ',' || !f.HasHeader {
		t.Fatalf("delimiter %q, header %v", f.Delimiter, f.HasHeader)
	}
	m := f.GuessMapping()
	// "oops" isn't a date: the layout is the one of the other rows, "oops" is an error
	if m.Date != 0 || m.DateLayout != "2006-01-02 15:04:05" {
		t.Fatalf("date column %d, layout %q", m.Date, m.DateLayout)
	}
	if m.Columns[Weight] != 1 || m.Columns[Fat] != 2 || m.Columns[Bone] != 3 || m.Columns[Muscle] != 4 ||
		m.Columns[BodyWater] != 5 || !m.FromMass[Fat] || !m.FromMass[Muscle] || !m.FromMass[BodyWater] || m.FromMass[Bone] {
		t.Fatalf("mapping %+v", m)
	}
	result, err := f.Measures(m)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Measures) != 2 || result.Empty != 1 || len(result.Errors) != 1 || result.Errors[0].Line != 5 {
		t.Fatalf("%d measures, %d empty, errors %+v", len(result.Measures), result.Empty, result.Errors)
	}
	// oldest first, with the line of each
	if result.Lines[0] != 3 || result.Lines[1] != 2 {
		t.Errorf("lines %v", result.Lines)
	}
	full := result.Measures[1]
	if !near(full.Weight, 72) || !near(full.Fat, 20) || !near(full.Muscle, 80) || !near(full.BodyWater, 55) || !near(full.Bone, 3.1) {
		t.Errorf("percentages of the weight: %+v", full)
	}
}

func TestCSVFrenchSpreadsheet(t *testing.T) {
	f := readCSV(t, "Date;Poids;Masse grasse (%);FC repos;VO2max\n28/10/2026;72,5;18,2;47;52\n01/10/2026;73;;;\n")
	if f.Delimiter != ';' {
		t.Fatalf("delimiter %q", f.Delimiter)
	}
	m := f.GuessMapping()
	if m.DateLayout != "02/01/2006" || m.Columns[Weight] != 1 || m.Columns[Fat] != 2 || m.FromMass[Fat] ||
		m.Columns[RestHR] != 3 || m.Columns[Vo2Max] != 4 {
		t.Fatalf("mapping %+v", m)
	}
	result, err := f.Measures(m)
	if err != nil || len(result.Measures) != 2 {
		t.Fatalf("%+v, %v", result, err)
	}
	if last := result.Measures[1]; !near(last.Weight, 72.5) || !near(last.Fat, 18.2) || last.RestHR != 47 ||
		last.Vo2Max != 52 || last.Date.Month() != time.October || last.Date.Day() != 28 {
		t.Errorf("%+v", last)
	}
}

func TestCSVWithoutHeaderInPounds(t *testing.T) {
	f := readCSV(t, "2026-10-01,160\n2026-10-02,159.5\n")
	if f.HasHeader {
		t.Fatal("a row of dates read as a header")
	}
	m := f.GuessMapping()
	if m.Date != 0 || m.DateLayout != "2006-01-02" {
		t.Fatalf("mapping %+v", m)
	}
	m.Columns[Weight], m.Pounds = 1, true
	result, err := f.Measures(m)
	if err != nil || len(result.Measures) != 2 || !near(result.Measures[0].Weight, 72.57) {
		t.Fatalf("%+v, %v", result, err)
	}
}

func TestCSVBloodPressure(t *testing.T) {
	// the bp.csv of Withings: the heart rate isn't the resting one
	f := readCSV(t, "Date,Heart rate,Systolic,Diastolic,Comments\n\"2026-10-05 08:00:00\",64,121,78,\n")
	m := f.GuessMapping()
	if m.Columns[RestHR] != NoColumn || m.Columns[Systolic] != 2 || m.Columns[Diastolic] != 3 {
		t.Fatalf("mapping %+v", m)
	}
	result, err := f.Measures(m)
	if err != nil || len(result.Measures) != 1 || result.Measures[0].Systolic != 121 || result.Measures[0].Diastolic != 78 {
		t.Fatalf("%+v, %v", result, err)
	}
	m.Columns[Diastolic] = NoColumn
	if _, err := f.Measures(m); err == nil {
		t.Error("a systolic column without diastolic was accepted")
	}
}

func TestContainsWord(t *testing.T) {
	if containsWord("physical activity", []string{"sys"}) || !containsWord("sys (mmhg)", []string{"sys"}) ||
		containsWord("albumin", []string{"lb"}) || !containsWord("weight (lb)", []string{"lb"}) {
		t.Error("words matched inside other words")
	}
}

func TestDetectDateLayout(t *testing.T) {
	f := readCSV(t, "Date,Weight\n13/10/2026,72\n01/10/2026,72.5\n")
	// 13 can't be a month: day first, as 01/10 then
	if layout := f.DetectDateLayout(0); layout != "02/01/2006" {
		t.Errorf("day first: %q", layout)
	}
	f = readCSV(t, "Date,Weight\n10/13/2026,72\n10/01/2026,72.5\n")
	if layout := f.DetectDateLayout(0); layout != "01/02/2006" {
		t.Errorf("month first: %q", layout)
	}
	// weights aren't dates, even as unix times
	if layout := f.DetectDateLayout(1); layout != "" {
		t.Errorf("weights read as %q dates", layout)
	}
}
