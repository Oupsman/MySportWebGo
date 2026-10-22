// Command gen builds cities15000.tsv.gz from the GeoNames cities15000 extract:
//
//	curl -O https://download.geonames.org/export/dump/cities15000.zip && unzip cities15000.zip
//	go run ./internal/pkg/cities/gen cities15000.txt internal/pkg/cities/cities15000.tsv.gz
//
// GeoNames data is licensed under CC BY 4.0, https://www.geonames.org/
package main

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// column indexes of the GeoNames "geoname" table
const (
	colName        = 1
	colLatitude    = 4
	colLongitude   = 5
	colFeatureCode = 7
	colCountry     = 8
	colAdmin1      = 10 // admin1 to admin4: 10 to 13
	colAdmin4      = 13
	colPopulation  = 14
)

type place struct {
	name, latitude, longitude, featureCode string
	municipality                           string // country and admin codes, "" without admin4 code
	population                             int
}

// isSeat tells if the place is the seat of an administrative division, a city rather than a district
func (p place) isSeat() bool {
	return p.featureCode == "PPLC" || strings.HasPrefix(p.featureCode, "PPLA")
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: gen cities15000.txt cities15000.tsv.gz")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(input, output string) error {
	in, err := os.Open(input)
	if err != nil {
		return err
	}
	defer in.Close()

	var places []place
	// the city of each municipality: its most populous seat
	seats := map[string]place{}
	scanner := bufio.NewScanner(in)
	for scanner.Scan() {
		cols := strings.Split(scanner.Text(), "\t")
		if len(cols) <= colPopulation {
			return fmt.Errorf("unexpected line %q", scanner.Text())
		}
		population, err := strconv.Atoi(cols[colPopulation])
		if err != nil {
			return fmt.Errorf("unexpected line %q", scanner.Text())
		}
		p := place{name: cols[colName], latitude: cols[colLatitude], longitude: cols[colLongitude],
			featureCode: cols[colFeatureCode], population: population}
		if cols[colAdmin4] != "" {
			p.municipality = cols[colCountry] + "|" + strings.Join(cols[colAdmin1:colAdmin4+1], "|")
		}
		places = append(places, p)
		if seat, ok := seats[p.municipality]; p.municipality != "" && p.isSeat() && (!ok || p.population > seat.population) {
			seats[p.municipality] = p
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	out, err := os.Create(output)
	if err != nil {
		return err
	}
	defer out.Close()
	zw, err := gzip.NewWriterLevel(out, gzip.BestCompression)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(zw)
	kept := 0
	for _, p := range places {
		// the districts of a city ("Paris 15 Vaugirard", "Salpêtrière") are named after it,
		// their points cover the city's area
		if seat, ok := seats[p.municipality]; ok {
			p.name = seat.name
		} else if p.featureCode == "PPLX" || p.featureCode == "PPLH" {
			// neighbourhoods and historical places: the city they belong to is in the file
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", p.name, p.latitude, p.longitude)
		kept++
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	fmt.Printf("%d places kept out of %d\n", kept, len(places))
	return nil
}
