package activityService

import (
	"MySportWeb/internal/pkg/types"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/muktihari/fit/profile/basetype"
	"github.com/muktihari/fit/profile/filedef"
	"github.com/muktihari/fit/profile/typedef"
)

// garminSuffixes are the parts of Garmin product names telling variants of the same device
var garminSuffixes = regexp.MustCompile(`_(china|japan|korea|taiwan|asia|apac|sea|india|single_byte_product_id)\b`)

// garminFamilies are the abbreviations of Garmin product names, written in full
var garminFamilies = map[string]string{
	"fr":   "Forerunner",
	"hrm":  "HRM",
	"vivo": "Vivo",
}

// AnalyzeDevice returns the device that recorded an activity, and its name ("Garmin Edge 530"),
// empty when the file doesn't tell. The file_id message names the device, the device_info of index 0
// (the creator) gives its software version.
func AnalyzeDevice(fit *filedef.Activity) (types.Device, string) {
	var device types.Device
	id := fit.FileId
	device.Brand = readableName(id.Manufacturer.String())
	device.Model = deviceModel(id.Manufacturer, id.Product, id.ProductName)
	if id.SerialNumber != basetype.Uint32zInvalid && id.SerialNumber != 0 {
		device.Serial = strconv.FormatUint(uint64(id.SerialNumber), 10)
	}
	for _, info := range fit.DeviceInfos {
		if info.DeviceIndex != typedef.DeviceIndexCreator {
			continue
		}
		if device.Model == "" {
			device.Model = deviceModel(info.Manufacturer, info.Product, info.ProductName)
		}
		if version := info.SoftwareVersionScaled(); !math.IsNaN(version) && version > 0 {
			device.Software = strconv.FormatFloat(version, 'f', 2, 64)
		}
		break
	}
	if strings.Contains(id.Manufacturer.String(), "Invalid") {
		device.Brand = ""
	}
	return device, DeviceName(device)
}

// DeviceName is the brand and the model of a device, once when the model already starts with the brand
func DeviceName(device types.Device) string {
	if device.Model == "" {
		return device.Brand
	}
	if device.Brand == "" || strings.HasPrefix(strings.ToLower(device.Model), strings.ToLower(device.Brand)) {
		return device.Model
	}
	return device.Brand + " " + device.Model
}

// deviceModel returns the name the device gives itself, else the name of its Garmin product
func deviceModel(manufacturer typedef.Manufacturer, product uint16, productName string) string {
	if name := strings.TrimSpace(productName); name != "" {
		return name
	}
	switch manufacturer {
	case typedef.ManufacturerGarmin, typedef.ManufacturerDynastream, typedef.ManufacturerDynastreamOem, typedef.ManufacturerTacx:
		name := typedef.GarminProduct(product).String()
		if strings.Contains(name, "Invalid") {
			return ""
		}
		return garminProductName(name)
	}
	return ""
}

// garminProductName writes a Garmin product name like the device: edge500 is "Edge 500",
// fr310xt "Forerunner 310XT", fenix6x_pro "Fenix 6X Pro"
func garminProductName(name string) string {
	name = garminSuffixes.ReplaceAllString(name, "")
	var words []string
	for _, part := range strings.Split(name, "_") {
		// the letters, then the number and its letters: edge|500, fr|310xt
		cut := strings.IndexFunc(part, unicode.IsDigit)
		if cut > 0 {
			words = append(words, family(part[:cut]), strings.ToUpper(part[cut:]))
		} else if part != "" {
			words = append(words, family(part))
		}
	}
	return strings.Join(words, " ")
}

func family(word string) string {
	if full, ok := garminFamilies[word]; ok {
		return full
	}
	return capitalize(word)
}

// readableName writes a FIT name like wahoo_fitness as "Wahoo Fitness"
func readableName(name string) string {
	words := strings.Split(name, "_")
	for i, word := range words {
		words[i] = capitalize(word)
	}
	return strings.Join(words, " ")
}

func capitalize(word string) string {
	if word == "" {
		return word
	}
	return strings.ToUpper(word[:1]) + word[1:]
}
