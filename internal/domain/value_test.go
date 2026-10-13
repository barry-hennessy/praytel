package domain_test

import (
	"fmt"

	"github.com/barry-hennessy/praytel/internal/domain"
)

func ExampleValue() {
	v := domain.ArrayValue([]domain.Value{
		domain.StringValue("ready"),
		domain.Int64Value(3),
		domain.BoolValue(true),
	})
	fmt.Println(v.Kind(), v)
	// Output:
	// array [ready, 3, true]
}

func ExampleLogLine_structuredAttributes() {
	line := domain.LogLine{
		Body: domain.StringValue(`level=info attempts=2`),
		Attributes: []domain.Attr{
			{Key: "level", Value: domain.StringValue("info")},
			{Key: "attempts", Value: domain.Int64Value(2)},
		},
	}
	fmt.Printf("%s: %s=%s, %s=%s\n", line.Body, line.Attributes[0].Key,
		line.Attributes[0].Value, line.Attributes[1].Key, line.Attributes[1].Value)
	// Output:
	// level=info attempts=2: level=info, attempts=2
}
