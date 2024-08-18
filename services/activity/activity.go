package activity

import (
	"MySportWeb/internal/pkg/models"
	"github.com/muktihari/fit/decoder"
	"os"
	"sync"
)

var decoderPool = sync.Pool{New: func() any { return decoder.New(nil) }}

func SumAnalyze(filePath string) (models.Activity, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return models.Activity{}, err
	}
	defer f.Close()

	dec := decoder.NewRaw()

	return models.Activity{}, nil
}
