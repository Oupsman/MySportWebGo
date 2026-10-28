package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"MySportWebSync/internal/api"
	"MySportWebSync/internal/health"
)

// previewRows is the number of measures shown before a CSV import
const previewRows = 8

func (u *UI) healthTab() fyne.CanvasObject {
	intro := widget.NewLabel("Import your health measures into MySportWeb: weight, body fat, muscle, bone, body water, " +
		"resting heart rate, VO2max and blood pressure. Importing a file again adds nothing: a measure of the same " +
		"date is completed, never duplicated. The device token needs the healthdatas:create permission.")
	intro.Wrapping = fyne.TextWrapWord
	apple := widget.NewButtonWithIcon("Apple Health export (.zip)…", theme.FolderOpenIcon(), func() {
		u.chooseFile([]string{".zip"}, u.importApple)
	})
	csvButton := widget.NewButtonWithIcon("CSV file…", theme.FileTextIcon(), func() {
		u.chooseFile([]string{".csv", ".txt", ".tsv"}, u.importCSV)
	})
	help := widget.NewLabel("Apple Health: in the Health application of the iPhone, your profile picture, " +
		"Export All Health Data, then copy export.zip to this computer.\n" +
		"CSV: an export of another application (Withings, Garmin, a spreadsheet…), you choose its columns.")
	help.Wrapping = fyne.TextWrapWord
	help.Importance = widget.LowImportance
	return container.NewVScroll(container.NewVBox(intro, container.NewHBox(apple, csvButton), help))
}

// chooseFile asks for a file of one of the extensions, from the home directory
func (u *UI) chooseFile(extensions []string, open func(path string)) {
	d := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, u.window)
			return
		}
		if r == nil {
			return // cancelled
		}
		path := r.URI().Path()
		r.Close()
		open(path)
	}, u.window)
	d.SetFilter(storage.NewExtensionFileFilter(extensions))
	if home, err := os.UserHomeDir(); err == nil {
		if lister, err := storage.ListerForURI(storage.NewFileURI(home)); err == nil {
			d.SetLocation(lister)
		}
	}
	d.Resize(u.window.Canvas().Size())
	d.Show()
}

// importWindow is the window of an import, beside the main one
func (u *UI) importWindow(title string) fyne.Window {
	w := u.app.NewWindow(title)
	w.Resize(fyne.NewSize(720, 600))
	return w
}

func (u *UI) importApple(path string) {
	w := u.importWindow("Apple Health import")
	progress := widget.NewProgressBar()
	w.SetContent(container.NewVBox(widget.NewLabel("Reading "+path+"…"), progress))
	w.Show()
	go func() {
		export, err := health.ReadAppleExport(path, func(done, total int64) {
			if total > 0 {
				fyne.Do(func() { progress.SetValue(float64(done) / float64(total)) })
			}
		})
		fyne.Do(func() {
			if err != nil {
				w.Close()
				dialog.ShowError(err, u.window)
				return
			}
			u.appleSelection(w, export)
		})
	}()
}

// appleSelection lets the user choose, among the data MySportWeb stores, the ones to import
func (u *UI) appleSelection(w fyne.Window, export *health.AppleExport) {
	sel := health.AppleSelection{Fields: map[health.Field]bool{}, Sources: map[string]bool{}}
	summary := widget.NewLabel("")
	summary.TextStyle.Bold = true
	importButton := widget.NewButtonWithIcon("Import", theme.UploadIcon(), nil)
	importButton.Importance = widget.HighImportance
	var result health.AppleResult
	update := func() {
		result = export.Measures(sel)
		text := fmt.Sprintf("%s to import", plural(len(result.Measures), "measure"))
		if result.UnpairedPressures > 0 {
			text += fmt.Sprintf(" (%s without the other value left out)", plural(result.UnpairedPressures, "blood pressure"))
		}
		summary.SetText(text)
		if len(result.Measures) == 0 {
			importButton.Disable()
		} else {
			importButton.Enable()
		}
	}

	content := container.NewVBox()
	if len(export.Kinds) == 0 {
		content.Add(widget.NewLabelWithStyle("This export has none of the data MySportWeb stores.",
			fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
	} else {
		content.Add(widget.NewLabelWithStyle("Data MySportWeb stores", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
		for _, kind := range export.Kinds {
			field := kind.Field
			sel.Fields[field] = true
			check := widget.NewCheck(fmt.Sprintf("%s — %s, %s → %s", kind.Field, plural(kind.Count, "value"),
				kind.First.Format("Jan 2, 2006"), kind.Last.Format("Jan 2, 2006")), func(on bool) {
				sel.Fields[field] = on
				update()
			})
			check.SetChecked(true)
			content.Add(check)
		}
	}
	if export.Skipped > 0 {
		skipped := widget.NewLabel(fmt.Sprintf("%s in an unknown unit or with an invalid date left out.",
			plural(export.Skipped, "record")))
		skipped.Importance = widget.WarningImportance
		content.Add(skipped)
	}

	for _, source := range export.Sources {
		sel.Sources[source] = true
	}
	if len(export.Sources) > 1 {
		content.Add(widget.NewLabelWithStyle("Sources", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
		for _, source := range export.Sources {
			source := source
			check := widget.NewCheck(source, func(on bool) {
				sel.Sources[source] = on
				update()
			})
			check.SetChecked(true)
			content.Add(check)
		}
	}

	since := widget.NewEntry()
	since.SetPlaceHolder("YYYY-MM-DD, empty for all")
	since.Validator = func(text string) error {
		if text = strings.TrimSpace(text); text == "" {
			return nil
		}
		_, err := time.ParseInLocation("2006-01-02", text, time.Local)
		return err
	}
	since.OnChanged = func(text string) {
		sel.Since = time.Time{}
		if t, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(text), time.Local); err == nil {
			sel.Since = t
		}
		update()
	}
	content.Add(widget.NewForm(widget.NewFormItem("Since", since)))

	if len(export.Others) > 0 {
		names := make([]string, 0, 8)
		for _, other := range export.Others[:min(8, len(export.Others))] {
			names = append(names, fmt.Sprintf("%s (%d)", other.Name, other.Count))
		}
		text := "Not stored by MySportWeb: " + strings.Join(names, ", ")
		if more := len(export.Others) - len(names); more > 0 {
			text += fmt.Sprintf(", and %s", plural(more, "other kind"))
		}
		others := widget.NewLabel(text)
		others.Wrapping = fyne.TextWrapWord
		others.Importance = widget.LowImportance
		content.Add(others)
	}

	cancel := widget.NewButton("Cancel", w.Close)
	importButton.OnTapped = func() {
		u.sendHealth(w, "Apple Health", result.Measures, func(i int) string {
			return result.Measures[i].Date.Format("2006-01-02 15:04")
		})
	}
	update()
	w.SetContent(container.NewBorder(nil, container.NewVBox(widget.NewSeparator(), summary,
		container.NewHBox(cancel, importButton)), nil, nil, container.NewVScroll(content)))
}

func (u *UI) importCSV(path string) {
	file, err := health.ReadCSV(path)
	if err != nil {
		dialog.ShowError(err, u.window)
		return
	}
	w := u.importWindow("CSV import")
	u.csvMapping(w, path, file)
	w.Show()
}

var delimiterNames = map[rune]string{',': "Comma ,", ';': "Semicolon ;", '\t': "Tab", '|': "Pipe |"}

// csvMapping lets the user choose the columns of the measures, the guessed ones first
func (u *UI) csvMapping(w fyne.Window, path string, file *health.CSVFile) {
	mapping := file.GuessMapping()
	none := "—"
	columns := []string{none}
	for i, name := range file.Header {
		label := fmt.Sprintf("%d: %s", i+1, name)
		if len(file.Rows) > 0 && i < len(file.Rows[0]) && !file.HasHeader {
			label += fmt.Sprintf(" (%s)", file.Rows[0][i])
		}
		columns = append(columns, label)
	}
	columnSelect := func(selected health.Column, set func(health.Column)) *widget.Select {
		s := widget.NewSelect(columns, nil)
		s.SetSelectedIndex(selected + 1)
		s.OnChanged = func(string) { set(s.SelectedIndex() - 1) }
		return s
	}

	preview := widget.NewLabel("")
	preview.TextStyle.Monospace = true
	preview.Wrapping = fyne.TextWrapWord
	summary := widget.NewLabel("")
	summary.TextStyle.Bold = true
	summary.Wrapping = fyne.TextWrapWord
	importButton := widget.NewButtonWithIcon("Import", theme.UploadIcon(), nil)
	importButton.Importance = widget.HighImportance
	var result health.CSVResult
	update := func() {
		var err error
		result, err = file.Measures(mapping)
		if err != nil {
			summary.SetText(err.Error())
			preview.SetText("")
			importButton.Disable()
			return
		}
		text := plural(len(result.Measures), "measure") + " to import"
		if result.Empty > 0 {
			text += fmt.Sprintf(", %s without value", plural(result.Empty, "row"))
		}
		if len(result.Errors) > 0 {
			text += fmt.Sprintf(", %s left out:", plural(len(result.Errors), "row"))
			for _, e := range result.Errors[:min(3, len(result.Errors))] {
				text += fmt.Sprintf("\nline %d: %v", e.Line, e.Err)
			}
		}
		summary.SetText(text)
		var lines []string
		for _, m := range result.Measures[:min(previewRows, len(result.Measures))] {
			lines = append(lines, describeMeasure(m))
		}
		if len(result.Measures) > previewRows {
			lines = append(lines, "…")
		}
		preview.SetText(strings.Join(lines, "\n"))
		if len(result.Measures) == 0 {
			importButton.Disable()
		} else {
			importButton.Enable()
		}
	}

	// the file read again with another delimiter or header: the mapping is guessed again
	delimiters := make([]string, 0, len(health.Delimiters))
	for _, d := range health.Delimiters {
		delimiters = append(delimiters, delimiterNames[d])
	}
	delimiter := widget.NewSelect(delimiters, nil)
	delimiter.SetSelected(delimiterNames[file.Delimiter])
	header := widget.NewCheck("The first row names the columns", nil)
	header.SetChecked(file.HasHeader)
	reparse := func() {
		d := health.Delimiters[delimiter.SelectedIndex()]
		if err := file.Parse(d, header.Checked); err != nil {
			dialog.ShowError(err, w)
			return
		}
		u.csvMapping(w, path, file)
	}
	delimiter.OnChanged = func(string) { reparse() }
	header.OnChanged = func(bool) { reparse() }

	layouts := make([]string, len(health.DateLayouts))
	for i, l := range health.DateLayouts {
		layouts[i] = l.Label
	}
	dateLayout := widget.NewSelect(layouts, nil)
	for i, l := range health.DateLayouts {
		if l.Layout == mapping.DateLayout {
			dateLayout.SetSelectedIndex(i)
		}
	}
	dateLayout.OnChanged = func(string) {
		mapping.DateLayout = health.DateLayouts[dateLayout.SelectedIndex()].Layout
		update()
	}
	dateColumn := columnSelect(mapping.Date, func(c health.Column) {
		mapping.Date = c
		// another column: its format
		if layout := file.DetectDateLayout(c); layout != "" {
			mapping.DateLayout = layout
			for i, l := range health.DateLayouts {
				if l.Layout == layout {
					dateLayout.SetSelectedIndex(i) // no OnChanged when it was already selected
				}
			}
		}
		update()
	})

	form := widget.NewForm(
		widget.NewFormItem("Delimiter", delimiter),
		widget.NewFormItem("", header),
		widget.NewFormItem("Date", dateColumn),
		widget.NewFormItem("Date format", dateLayout),
	)
	for _, field := range health.Fields {
		field := field
		selectColumn := columnSelect(mapping.Columns[field], func(c health.Column) {
			mapping.Columns[field] = c
			update()
		})
		var item fyne.CanvasObject = selectColumn
		if isPercentage(field) {
			unit := widget.NewSelect([]string{"%", "mass, part of the weight"}, nil)
			if mapping.FromMass[field] {
				unit.SetSelectedIndex(1)
			} else {
				unit.SetSelectedIndex(0)
			}
			unit.OnChanged = func(string) {
				mapping.FromMass[field] = unit.SelectedIndex() == 1
				update()
			}
			item = container.NewGridWithColumns(2, selectColumn, unit)
		}
		form.Append(fmt.Sprintf("%s (%s)", field, field.Unit()), item)
	}
	masses := widget.NewSelect([]string{"kg", "lb"}, nil)
	if mapping.Pounds {
		masses.SetSelectedIndex(1)
	} else {
		masses.SetSelectedIndex(0)
	}
	masses.OnChanged = func(string) {
		mapping.Pounds = masses.SelectedIndex() == 1
		update()
	}
	form.Append("Masses in", masses)

	cancel := widget.NewButton("Cancel", w.Close)
	importButton.OnTapped = func() {
		u.sendHealth(w, "CSV "+pathBase(path), result.Measures, func(i int) string {
			return fmt.Sprintf("line %d", result.Lines[i])
		})
	}
	update()
	content := container.NewVBox(widget.NewLabel(path), form, widget.NewSeparator(),
		widget.NewLabelWithStyle("Preview", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), preview)
	w.SetContent(container.NewBorder(nil, container.NewVBox(widget.NewSeparator(), summary,
		container.NewHBox(cancel, importButton)), nil, nil, container.NewVScroll(content)))
}

func isPercentage(f health.Field) bool {
	for _, p := range health.Percentages {
		if p == f {
			return true
		}
	}
	return false
}

// describeMeasure is a line of the preview: "2026-10-01 07:30  Weight 72.4 kg  Fat 18.3 %"
func describeMeasure(m health.Measure) string {
	parts := []string{m.Date.Format("2006-01-02 15:04")}
	for _, f := range health.Fields {
		if v := m.Get(f); v != 0 {
			parts = append(parts, fmt.Sprintf("%s %s %s", f, strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1f", v), "0"), "."), f.Unit()))
		}
	}
	return strings.Join(parts, "  ")
}

// sendHealth imports measures and tells the result; where(i) names the measure i for the refused ones
func (u *UI) sendHealth(w fyne.Window, source string, measures []health.Measure, where func(i int) string) {
	cfg := u.manager.Config()
	client, err := api.New(cfg.ServerURL, cfg.Token)
	if err != nil {
		dialog.ShowError(fmt.Errorf("settings: %w", err), w)
		return
	}
	progress := widget.NewProgressBar()
	w.SetContent(container.NewVBox(widget.NewLabel(fmt.Sprintf("Sending %s…", plural(len(measures), "measure"))), progress))
	go func() {
		ctx := context.Background()
		var result api.HealthImport
		// a token without the permission is told before sending anything
		info, err := client.CheckToken(ctx)
		if err == nil && !info.CanImportHealth() {
			err = fmt.Errorf("the token %q lacks the healthdatas:create permission: create a token with it in MySportWeb", info.Name)
		} else if errors.Is(err, api.ErrNoTokenRoute) {
			err = nil
		}
		if err == nil {
			result, err = client.ImportHealth(ctx, measures, func(done, total int) {
				fyne.Do(func() { progress.SetValue(float64(done) / float64(total)) })
			})
		}
		fyne.Do(func() {
			if err != nil {
				u.manager.Log(source, "health import: "+err.Error(), true)
				dialog.ShowError(err, w)
				w.SetContent(container.NewVBox(widget.NewLabel("The import failed: "+err.Error()), widget.NewButton("Close", w.Close)))
				return
			}
			text := fmt.Sprintf("%s created, %s completed, %s already imported", plural(result.Created, "measure"),
				plural(result.Merged, "measure"), plural(result.Unchanged, "measure"))
			if len(result.Rejected) > 0 {
				text += fmt.Sprintf(", %s refused", plural(len(result.Rejected), "measure"))
			}
			u.manager.Log(source, "health import: "+text, len(result.Rejected) > 0)
			lines := []string{text}
			for _, r := range result.Rejected[:min(10, len(result.Rejected))] {
				lines = append(lines, fmt.Sprintf("%s: %s", where(r.Index), r.Error))
			}
			label := widget.NewLabel(strings.Join(lines, "\n"))
			label.Wrapping = fyne.TextWrapWord
			w.SetContent(container.NewBorder(nil, widget.NewButton("Close", w.Close), nil, nil, container.NewVScroll(label)))
		})
	}()
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func pathBase(p string) string {
	return p[strings.LastIndexAny(p, `/\`)+1:]
}
