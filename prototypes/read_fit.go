package main

import (
	"fmt"
	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/profile/filedef"
	"os"
)

func main() {
	// filePath := "FIT/Karoo-Morning_Ride-Sep-06-2022-061544.fit"
	filePath := "116201380_UNKNOWN.fit"
	f, err := os.Open(filePath)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	dec := decoder.New(f, decoder.WithIgnoreChecksum())
	// Read the fit file

	for dec.Next() {
		fit, err := dec.Decode()
		fmt.Println("Decoder")
		if err != nil {
			panic(err)
		}
		activity := filedef.NewActivity(fit.Messages...)
		fmt.Printf("File Type: %s\n", activity.FileId.Type)
		fmt.Printf("EnhancedAvgSpeedScaled: %f\n", activity.Sessions[0].EnhancedAvgSpeedScaled())
		fmt.Printf("AvgSpeedScaled: %f\n", activity.Sessions[0].AvgSpeedScaled())
		fmt.Printf("EnhancedAvgSpeed: %d\n", activity.Sessions[0].EnhancedAvgSpeed)
		fmt.Printf("AvgSpeed: %d\n", activity.Sessions[0].AvgSpeed)
		fmt.Printf("Timestamp: %d\n", activity.Records[100].Timestamp)
	}

}
