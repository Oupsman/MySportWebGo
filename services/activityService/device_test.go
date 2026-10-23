package activityService

import (
	"MySportWeb/internal/pkg/types"
	"os"
	"path/filepath"
	"testing"

	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/profile/filedef"
)

func TestGarminProductName(t *testing.T) {
	for name, want := range map[string]string{
		"edge500":                      "Edge 500",
		"edge500_taiwan":               "Edge 500",
		"fr310xt":                      "Forerunner 310XT",
		"fr225_single_byte_product_id": "Forerunner 225",
		"fenix6x":                      "Fenix 6X",
		"fenix6x_pro":                  "Fenix 6X Pro",
		"edge_1030_plus":               "Edge 1030 Plus",
		"vivo_active":                  "Vivo Active",
	} {
		if got := garminProductName(name); got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
}

func TestDeviceName(t *testing.T) {
	for _, test := range []struct {
		device types.Device
		want   string
	}{
		{types.Device{Brand: "Garmin", Model: "Edge 530"}, "Garmin Edge 530"},
		// the model already names the brand
		{types.Device{Brand: "Wahoo Fitness", Model: "Wahoo Fitness ELEMNT BOLT"}, "Wahoo Fitness ELEMNT BOLT"},
		{types.Device{Brand: "Coros"}, "Coros"},
		{types.Device{Model: "Karoo"}, "Karoo"},
		{types.Device{}, ""},
	} {
		if got := DeviceName(test.device); got != test.want {
			t.Errorf("%+v: %q, want %q", test.device, got, test.want)
		}
	}
	if got := readableName("wahoo_fitness"); got != "Wahoo Fitness" {
		t.Errorf("manufacturer %q", got)
	}
}

func TestAnalyzeDevice(t *testing.T) {
	for file, want := range map[string]types.Device{
		// Garmin names the product with a number, the name comes from the FIT profile
		"2022-08-30-18-12-37.fit": {Brand: "Garmin", Model: "Fenix 6X", Software: "22.10"},
		"116201380_UNKNOWN.fit":   {Brand: "Garmin", Model: "Edge 500", Software: "2.80"},
		// Hammerhead writes the name of the product in the file
		"0140d189-a2fd-4745-8e5f-10bdea544237.fit": {Brand: "Hammerhead", Model: "Karoo 2"},
		"37fdc366-b57c-4775-b4ec-50b4c48cf7c2.fit": {Brand: "Hammerhead", Model: "Karoo"},
	} {
		f, err := os.Open(filepath.Join("../../prototypes", file))
		if err != nil {
			t.Skip("FIT files missing in prototypes/")
		}
		fit, err := decoder.New(f, decoder.WithIgnoreChecksum()).Decode()
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		device, name := AnalyzeDevice(filedef.NewActivity(fit.Messages...))
		if device.Brand != want.Brand || device.Model != want.Model || device.Software != want.Software {
			t.Errorf("%s: %+v, want %+v", file, device, want)
		}
		if name != DeviceName(want) {
			t.Errorf("%s: name %q", file, name)
		}
	}
}
