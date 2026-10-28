// Package syncer watches the devices plugged in and uploads their new activities
package syncer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"MySportWebSync/internal/api"
	"MySportWebSync/internal/config"
	"MySportWebSync/internal/device"
	"MySportWebSync/internal/state"
)

const (
	pollInterval = 3 * time.Second
	maxEvents    = 500
)

// Event is a line of the history
type Event struct {
	At     time.Time
	Device string
	File   string
	Text   string
	Failed bool
}

// DeviceStatus is a plugged device as shown to the user
type DeviceStatus struct {
	ID, Name string
	Kind     device.Kind
	Syncing  bool
	Done     int // files handled in the current synchronization
	Total    int
	Last     string // the outcome of the last synchronization
	LastErr  bool
}

type job struct {
	id     string
	manual bool // the user asked: the files the server refused are tried again
}

// Manager scans the devices, and synchronizes them one at a time
type Manager struct {
	state    *state.State
	scanners []device.Scanner
	notices  func() []string
	onChange func()               // called after any change of Devices, Events or Notices
	onDone   func(summary string) // called at the end of each synchronization that uploaded something

	mu      sync.Mutex
	cfg     config.Config
	devices map[string]device.Device
	status  map[string]*DeviceStatus
	events  []Event
	queued  map[string]bool
	jobs    chan job
}

func NewManager(cfg config.Config, st *state.State, mtp *device.MTPScanner, onChange func(), onDone func(string)) *Manager {
	return &Manager{
		state:    st,
		scanners: []device.Scanner{device.NewFSScanner(), mtp},
		notices:  mtp.Notices,
		onChange: onChange,
		onDone:   onDone,
		cfg:      cfg,
		devices:  map[string]device.Device{},
		status:   map[string]*DeviceStatus{},
		queued:   map[string]bool{},
		jobs:     make(chan job, 32),
	}
}

func (m *Manager) Config() config.Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

func (m *Manager) SetConfig(cfg config.Config) {
	m.mu.Lock()
	m.cfg = cfg
	m.mu.Unlock()
}

// Run scans the devices until ctx is done
func (m *Manager) Run(ctx context.Context) {
	go m.worker(ctx)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		m.scan(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *Manager) scan(ctx context.Context) {
	cfg := m.Config()
	found := map[string]device.Device{}
	var order []string
	for _, scanner := range m.scanners {
		devices, err := scanner.Scan(ctx, cfg.Folders)
		if err != nil {
			if ctx.Err() == nil {
				m.event(Event{Text: "scanning the devices: " + err.Error(), Failed: true})
			}
			continue
		}
		for _, d := range devices {
			// the same device through GVfs and libmtp, or two storages: the first one wins
			if _, ok := found[d.ID()]; !ok {
				found[d.ID()] = d
				order = append(order, d.ID())
			}
		}
	}

	m.mu.Lock()
	changed := false
	for id := range m.devices {
		if _, ok := found[id]; !ok {
			delete(m.devices, id)
			if !m.status[id].Syncing {
				delete(m.status, id)
			}
			changed = true
		}
	}
	var plugged []string
	for _, id := range order {
		if _, ok := m.devices[id]; ok {
			m.devices[id] = found[id] // a new handle after a replug through another mode
			continue
		}
		d := found[id]
		m.devices[id] = d
		if _, ok := m.status[id]; !ok {
			m.status[id] = &DeviceStatus{ID: id, Name: d.Name(), Kind: d.Kind()}
		}
		plugged = append(plugged, id)
		changed = true
	}
	auto := m.cfg.AutoSync
	m.mu.Unlock()

	for _, id := range plugged {
		d := found[id]
		m.event(Event{Device: d.Name(), Text: fmt.Sprintf("plugged (%s)", d.Kind())})
		if auto {
			m.enqueue(job{id: id})
		}
	}
	if changed {
		m.onChange()
	}
}

// Sync queues the synchronization of a device asked by the user
func (m *Manager) Sync(id string) {
	m.enqueue(job{id: id, manual: true})
}

func (m *Manager) SyncAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.devices))
	for id := range m.devices {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Sync(id)
	}
}

func (m *Manager) enqueue(j job) {
	m.mu.Lock()
	if m.queued[j.id] {
		m.mu.Unlock()
		return
	}
	m.queued[j.id] = true
	m.mu.Unlock()
	m.jobs <- j
}

func (m *Manager) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-m.jobs:
			m.mu.Lock()
			delete(m.queued, j.id)
			d := m.devices[j.id]
			cfg := m.cfg
			m.mu.Unlock()
			if d != nil {
				m.syncDevice(ctx, cfg, d, j.manual)
			}
		}
	}
}

// Devices returns the plugged devices, by name
func (m *Manager) Devices() []DeviceStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := make([]DeviceStatus, 0, len(m.status))
	for _, s := range m.status {
		list = append(list, *s)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

// Events returns the history, the latest first
func (m *Manager) Events() []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	events := slices.Clone(m.events)
	slices.Reverse(events)
	return events
}

func (m *Manager) Notices() []string {
	return m.notices()
}

func (m *Manager) event(e Event) {
	e.At = time.Now()
	m.mu.Lock()
	m.events = append(m.events, e)
	if len(m.events) > maxEvents {
		m.events = slices.Delete(m.events, 0, len(m.events)-maxEvents)
	}
	m.mu.Unlock()
	m.onChange()
}

func (m *Manager) update(id string, fn func(s *DeviceStatus)) {
	m.mu.Lock()
	if s, ok := m.status[id]; ok {
		fn(s)
	}
	m.mu.Unlock()
	m.onChange()
}

func (m *Manager) syncDevice(ctx context.Context, cfg config.Config, d device.Device, manual bool) {
	m.update(d.ID(), func(s *DeviceStatus) { s.Syncing, s.Done, s.Total = true, 0, 0 })
	counts, err := m.upload(ctx, cfg, d, manual)
	d.Close()

	summary := counts.String()
	if err != nil {
		summary = err.Error()
		if counts.handled() > 0 {
			summary = counts.String() + ", then: " + err.Error()
		}
		m.event(Event{Device: d.Name(), Text: summary, Failed: true})
	} else {
		m.event(Event{Device: d.Name(), Text: "synchronized: " + summary})
	}
	m.mu.Lock()
	if s, ok := m.status[d.ID()]; ok {
		s.Syncing, s.Last, s.LastErr = false, summary, err != nil
		if _, plugged := m.devices[d.ID()]; !plugged {
			delete(m.status, d.ID())
		}
	}
	m.mu.Unlock()
	m.onChange()
	if counts.uploaded > 0 || err != nil {
		m.onDone(d.Name() + ": " + summary)
	}
}

type counts struct {
	uploaded, duplicates, rejected int
}

func (c counts) handled() int { return c.uploaded + c.duplicates + c.rejected }

func (c counts) String() string {
	if c.handled() == 0 {
		return "nothing new"
	}
	return fmt.Sprintf("%d uploaded, %d already on the server, %d refused", c.uploaded, c.duplicates, c.rejected)
}

func (m *Manager) upload(ctx context.Context, cfg config.Config, d device.Device, manual bool) (counts, error) {
	var c counts
	client, err := api.New(cfg.ServerURL, cfg.Token)
	if err != nil {
		return c, fmt.Errorf("settings: %w", err)
	}
	files, err := d.Files(ctx)
	if err != nil {
		return c, fmt.Errorf("listing the activities: %w", err)
	}

	var cutoff time.Time
	if cfg.MaxAgeDays > 0 {
		cutoff = time.Now().AddDate(0, 0, -cfg.MaxAgeDays)
	}
	var pending []device.File
	for _, f := range files {
		if f.ModTime.Before(cutoff) {
			continue
		}
		if r, ok := m.state.File(state.Key(d.ID(), f)); ok && (r.Result != state.Rejected || !manual) {
			continue
		}
		pending = append(pending, f)
	}
	m.update(d.ID(), func(s *DeviceStatus) { s.Total = len(pending) })

	sent := 0
	for i, f := range pending {
		if err := ctx.Err(); err != nil {
			return c, err
		}
		key := state.Key(d.ID(), f)
		var buf bytes.Buffer
		if err := d.Copy(ctx, f, &buf); err != nil {
			return c, fmt.Errorf("reading %s: %w", f.Name(), err)
		}
		sum := sha256.Sum256(buf.Bytes())
		hash := hex.EncodeToString(sum[:])

		if r, ok := m.state.Content(hash); ok {
			// read before from another mode or another device
			r.At = time.Now()
			if err := m.state.Put(key, r); err != nil {
				return c, fmt.Errorf("saving the state: %w", err)
			}
		} else {
			result, err := client.Upload(ctx, f.Name(), buf.Bytes(), sent, len(pending))
			if err != nil {
				if errors.Is(err, api.ErrAuth) {
					return c, fmt.Errorf("%w, check the token in the settings", err)
				}
				return c, fmt.Errorf("uploading %s: %w", f.Name(), err)
			}
			sent++
			r := state.Record{Hash: hash, ActivityID: result.ActivityID, Message: result.Message, At: time.Now()}
			e := Event{Device: d.Name(), File: f.Name()}
			switch result.Status {
			case api.Uploaded:
				c.uploaded++
				r.Result, e.Text = state.Uploaded, "uploaded"
			case api.Duplicate:
				c.duplicates++
				r.Result, e.Text = state.Duplicate, "already on the server"
			case api.Rejected:
				c.rejected++
				r.Result, e.Text, e.Failed = state.Rejected, "refused by the server: "+result.Message, true
			}
			if err := m.state.Put(key, r); err != nil {
				return c, fmt.Errorf("saving the state: %w", err)
			}
			m.event(e)
		}
		m.update(d.ID(), func(s *DeviceStatus) { s.Done = i + 1 })
	}
	return c, nil
}

// Log adds a line to the history, for the work done outside the synchronizations (health imports)
func (m *Manager) Log(source, text string, failed bool) {
	m.event(Event{Device: source, Text: text, Failed: failed})
}
