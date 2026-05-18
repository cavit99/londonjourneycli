package tfl

import "strings"

var knownStops = map[string]string{
	"heathrow t2":                "1000105",
	"heathrow terminal 2":        "1000105",
	"heathrow terminal 3":        "1000105",
	"heathrow terminals 2 and 3": "1000105",
	"heathrow terminals 2 & 3":   "1000105",
	"heathrow t4":                "910GHTROW4A",
	"heathrow terminal 4":        "910GHTROW4A",
	"heathrow t5":                "910GHTRWTM5",
	"heathrow terminal 5":        "910GHTRWTM5",
	"heathrow":                   "1000105",
	"lhr":                        "1000105",
	"paddington":                 "1000174",
	"london paddington":          "1000174",
	"victoria":                   "1000248",
	"london victoria":            "1000248",
	"waterloo":                   "1000254",
	"london waterloo":            "1000254",
	"london bridge":              "1000139",
	"kings cross":                "1000129",
	"king's cross":               "1000129",
	"st pancras":                 "1000129",
	"euston":                     "1000077",
	"london euston":              "1000077",
	"gatwick":                    "920GLGW0",
	"gatwick airport":            "920GLGW0",
	"stansted":                   "920GSTN1",
	"stansted airport":           "920GSTN1",
	"luton":                      "910GLUTOAPY",
	"luton airport":              "910GLUTOAPY",
	"london city airport":        "490G00007860",
	"city airport":               "490G00007860",
}

func ResolveKnownStop(s string) string {
	key := strings.Join(strings.Fields(strings.ToLower(s)), " ")
	if v, ok := knownStops[key]; ok {
		return v
	}
	return s
}
