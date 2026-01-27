package ranreport

import (
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/kelseyhightower/envconfig"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/types"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/reportxml"
)

type settings struct {
	IDTag        string `default:"test_id"`
	CaseTag      string `default:"testcase-id" envconfig:"REPORT_CASE_TAG"`
	ParameterTag string `default:"parameter" envconfig:"REPORT_PARAMETER_TAG"`
}

var config *settings

// CreateWithSuiteProperties writes a report to a given xml file with optional suite-level properties.
// This is an enhanced version of reportxml.Create that supports adding properties at the testsuite level.
func CreateWithSuiteProperties(report ginkgo.Report, destFile, projectTag string, suiteProperties map[string]string) {
	if destFile == "" {
		return
	}

	testSuite := setTestSuite(report)

	// Add suite-level properties
	for key, value := range suiteProperties {
		testSuite.Properties.Property = append(testSuite.Properties.Property, reportxml.Property{
			Name:  key,
			Value: value,
		})
	}

	for _, testCaseSpecReport := range report.SpecReports {
		if testCaseSpecReport.FullText() == "" {
			continue
		}

		testCase := reportxml.TestCase{
			Name: testCaseSpecReport.FullText(),
		}

		if testID := setTestID(testCaseSpecReport, projectTag); testID != nil {
			testCase.Properties.Property = append(testCase.Properties.Property, *testID)
		}

		if testTCProperties := setProperty(testCaseSpecReport); testTCProperties != nil {
			for _, property := range testTCProperties {
				testCase.Properties.Property = append(testCase.Properties.Property, *property)
			}
		}

		if failedMessage := setFailureMessage(testCaseSpecReport); failedMessage != nil {
			testCase.FailureMessage = failedMessage
		}

		if skippedMessage := setSkipMessage(testCaseSpecReport); skippedMessage != nil {
			testCase.Skipped = skippedMessage
		}

		testSuite.TestCases = append(testSuite.TestCases, testCase)
		testSuite.Tests++
	}

	generateReportXMLFile(destFile, testSuite)
}

func setTestSuite(report ginkgo.Report) *reportxml.TestSuite {
	return &reportxml.TestSuite{
		XMLName:  xml.Name{Space: report.SuiteDescription},
		Name:     report.SuiteDescription,
		Tests:    0,
		Time:     report.RunTime.Seconds(),
		Skipped:  report.SpecReports.CountWithState(types.SpecStateSkipped),
		Failures: report.SpecReports.CountWithState(types.SpecStateFailureStates),
	}
}

func setTestID(testReport types.SpecReport, projectTag string) *reportxml.Property {
	if len(testReport.Labels()) > 0 {
		for _, label := range testReport.Labels() {
			if strings.Contains(label, config.IDTag) {
				return &reportxml.Property{
					Name:  config.CaseTag,
					Value: fmt.Sprintf("%s%s", projectTag, strings.Split(label, ":")[1]),
				}
			}
		}
	}

	return nil
}

func setProperty(testReport types.SpecReport) []*reportxml.Property {
	if len(testReport.Labels()) > 0 {
		var tcProperties []*reportxml.Property

		for _, label := range testReport.Labels() {
			if strings.Contains(label, config.ParameterTag) {
				tcProperties = append(tcProperties, &reportxml.Property{
					Name:  strings.Split(label, ":")[0],
					Value: strings.Split(label, ":")[1],
				})
			}
		}

		if len(tcProperties) > 0 {
			return tcProperties
		}
	}

	return nil
}

func setFailureMessage(testReport types.SpecReport) *reportxml.FailureMessage {
	if types.SpecStateFailureStates.Is(testReport.State) {
		return &reportxml.FailureMessage{
			Type:    failureTypeForState(testReport.State),
			Message: failureMessage(testReport.Failure),
		}
	}

	return nil
}

func setSkipMessage(testReport types.SpecReport) *reportxml.Skipped {
	if types.SpecStateSkipped.Is(testReport.State) {
		return &reportxml.Skipped{
			XMLName: xml.Name{Space: testReport.Failure.Message},
			Message: testReport.Failure.Message,
		}
	}

	return nil
}

func createNewReportFile(outputFile string, testCases *reportxml.TestSuite) {
	file, err := os.Create(outputFile)
	if err != nil {
		panic(fmt.Errorf("failed to create report file: %s\n\t%w", outputFile, err))
	}

	defer func() {
		_ = file.Close()
	}()

	encoder := xml.NewEncoder(file)
	encoder.Indent("  ", "    ")

	err = encoder.Encode(testCases)
	if err != nil {
		panic(fmt.Errorf("failed to dump report to file: %w", err))
	}
}

func appendToExistingReportFile(outputFile string, newReport *reportxml.TestSuite) {
	file, err := os.OpenFile(outputFile, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		panic(fmt.Errorf("failed to open report file: %s\n\t%w", outputFile, err))
	}

	defer func() {
		_ = file.Close()
	}()

	existingTestSuiteByteFormat, err := os.ReadFile(outputFile)
	if err != nil {
		panic(fmt.Errorf("failed to read existing report file: %s\n\t%w", outputFile, err))
	}

	var reportTestSuite *reportxml.TestSuite

	err = xml.Unmarshal(existingTestSuiteByteFormat, &reportTestSuite)
	if err != nil {
		panic(fmt.Errorf("failed to unmarshal existing report file: %s\n\t%w", outputFile, err))
	}

	file, err = os.OpenFile(outputFile, os.O_RDWR|os.O_TRUNC, 0644)
	if err != nil {
		panic(fmt.Errorf("failed to open report file: %s\n\t%w", outputFile, err))
	}

	defer func() {
		_ = file.Close()
	}()

	reportTestSuite.Name = "Aggregated Report"
	reportTestSuite.TestCases = append(reportTestSuite.TestCases, newReport.TestCases...)
	reportTestSuite.Tests += newReport.Tests
	reportTestSuite.Skipped += newReport.Skipped
	reportTestSuite.Failures += newReport.Failures
	reportTestSuite.Time += newReport.Time

	// Merge suite-level properties from the new report
	reportTestSuite.Properties.Property = append(reportTestSuite.Properties.Property, newReport.Properties.Property...)

	encoder := xml.NewEncoder(file)
	encoder.Indent("  ", "    ")

	err = encoder.Encode(reportTestSuite)
	if err != nil {
		panic(fmt.Errorf("failed to generate aggregated report\n\t%w", err))
	}
}

func generateReportXMLFile(outputFile string, testCases *reportxml.TestSuite) {
	_, err := os.Stat(outputFile)
	if errors.Is(err, os.ErrNotExist) {
		createNewReportFile(outputFile, testCases)
	} else {
		appendToExistingReportFile(outputFile, testCases)
	}
}

func failureTypeForState(state types.SpecState) string {
	//nolint:exhaustive
	switch state {
	case types.SpecStateFailed:
		return "Failure"
	case types.SpecStateInterrupted:
		return "Interrupted"
	case types.SpecStatePanicked:
		return "Panic"
	default:
		return ""
	}
}

func failureMessage(failure types.Failure) string {
	return fmt.Sprintf(
		"%s\n%s\n%s", failure.FailureNodeLocation.String(), failure.Message, failure.Location.String())
}

func newConfig() (*settings, error) {
	var setting settings

	err := envconfig.Process("", &setting)
	if err != nil {
		return nil, err
	}

	setting.setDefaultTag()

	return &setting, nil
}

func (set *settings) setDefaultTag() {
	typ := reflect.TypeOf(*set)

	if set.CaseTag == "" {
		f, _ := typ.FieldByName("CaseTag")
		set.CaseTag = f.Tag.Get("default")
	}

	if set.ParameterTag == "" {
		f, _ := typ.FieldByName("ParameterTag")
		set.ParameterTag = f.Tag.Get("default")
	}

	if set.IDTag == "" {
		f, _ := typ.FieldByName("IDTag")
		set.IDTag = f.Tag.Get("default")
	}
}

//nolint:gochecknoinits
func init() {
	var err error

	config, err = newConfig()
	if err != nil {
		panic(fmt.Sprintf("Failed to init ranreport config. Error: %s", err.Error()))
	}
}
