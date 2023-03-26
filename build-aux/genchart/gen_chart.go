// gen_chart.go is essentially poor-man's `helm package`.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/telepresenceio/telepresence/v2/charts"
	"github.com/telepresenceio/telepresence/v2/pkg/version"
)

const (
	telepresenceChartName     = "telepresence"
	telepresenceCRDsChartName = "telepresence-crds"
)

func main() {
	var version string
	if len(os.Args) > 2 {
		version = os.Args[2]
	}
	for _, chartName := range []string{telepresenceChartName, telepresenceCRDsChartName} {
		if err := GenerateChart(chartName, os.Args[1], version); err != nil {
			fmt.Fprintf(os.Stderr, "%s: error: %v\n", os.Args[0], err)
			os.Exit(1)
		}
	}
}

func GenerateChart(chartName, dstDir, verStr string) (err error) {
	maybeSetErr := func(_err error) {
		if err == nil && _err != nil {
			err = _err
		}
	}

	if verStr == "" {
		verStr = version.Version
	}
	verStr = strings.TrimPrefix(verStr, "v")
	fh, err := os.OpenFile(filepath.Join(dstDir, fmt.Sprintf("%s-%s.tgz", chartName, verStr)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o666)
	if err != nil {
		return err
	}
	defer func() {
		maybeSetErr(fh.Close())
	}()

	if err := charts.WriteChart(map[string]charts.DirType{
		telepresenceChartName:     charts.DirTypeTelepresence,
		telepresenceCRDsChartName: charts.DirTypeTelepresenceCRDs,
	}[chartName], fh, chartName, "v"+verStr); err != nil {
		return err
	}

	return nil
}
