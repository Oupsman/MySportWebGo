package device

import (
	"encoding/xml"
	"io"
	"strings"
)

// garminDeviceXML is GARMIN/GarminDevice.xml, written by the Garmin devices in mass storage and MTP mode
const garminDeviceXML = "GARMIN/GarminDevice.xml"

type garminInfo struct {
	ID    string // the unit id, the same in both modes: the activities of a device keep their state across modes
	Model string
}

// parseGarminDevice reads the unit id and the model, the elements are matched without their namespace
func parseGarminDevice(r io.Reader) (garminInfo, bool) {
	var doc struct {
		ID    string `xml:"Id"`
		Model struct {
			Description string `xml:"Description"`
		} `xml:"Model"`
	}
	if err := xml.NewDecoder(io.LimitReader(r, 1<<20)).Decode(&doc); err != nil {
		return garminInfo{}, false
	}
	info := garminInfo{ID: strings.TrimSpace(doc.ID), Model: strings.TrimSpace(doc.Model.Description)}
	return info, info.ID != ""
}
