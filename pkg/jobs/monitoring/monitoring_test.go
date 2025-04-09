package monitoring

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

func TestSetExtensionAndOverrideValues(t *testing.T) {
	var tests = []struct {
		name                     string
		ccHostFile               string
		ccMemberFile             string
		extensionValuesFile      string
		overrideHostValuesFile   string
		overrideMemberValuesFile string
	}{
		{
			ccHostFile:               "./tests/cc.host.yaml",
			ccMemberFile:             "./tests/cc.member.yaml",
			extensionValuesFile:      "./tests/extension.values.yaml",
			overrideHostValuesFile:   "./tests/override.host.values.yaml",
			overrideMemberValuesFile: "./tests/override.member.values.yaml",
		},
	}

	for _, test := range tests {
		// get host cc
		ccHostBytes, err := os.ReadFile(test.ccHostFile)
		if err != nil {
			t.Errorf("Failed to read from %s: %v", test.ccHostFile, err)
		}
		var ccHost = make(map[string]interface{})
		err = yaml.Unmarshal(ccHostBytes, &ccHost)
		if err != nil {
			t.Errorf("Failed to unmarshal host cc: %v", err)
		}
		// get member cc
		ccMemberBytes, err := os.ReadFile(test.ccMemberFile)
		if err != nil {
			t.Errorf("Failed to read from %s: %v", test.ccMemberFile, err)
		}
		var ccMember = make(map[string]interface{})
		err = yaml.Unmarshal(ccMemberBytes, &ccMember)
		if err != nil {
			t.Errorf("Failed to unmarshal member cc: %v", err)
		}
		// generate actuals
		transPaths := append(whizardTransPaths[:], stackTransPaths...)
		extensionValues, err := SetExtensionValues("", ccHost, transPaths)
		if err != nil {
			t.Errorf("Failed to generate extension values: %v", err)
		}
		transPaths = append(stackTransPaths[:], etcdTransPaths...)
		overrideHostValues, err := SetOverrideValues("", ccHost, transPaths, extensionValues)
		if err != nil {
			t.Errorf("Failed to generate host override values: %v", err)
		}
		var actualOverrideHost = make(map[string]interface{})
		err = yaml.Unmarshal([]byte(overrideHostValues), &actualOverrideHost)
		if err != nil {
			t.Errorf("Failed to unmarshal actual host override values: %v", err)
		}
		overrideMemberValues, err := SetOverrideValues("", ccMember, transPaths, extensionValues)
		if err != nil {
			t.Errorf("Failed to generate host override values: %v", err)
		}
		var actualOverrideMember = make(map[string]interface{})
		err = yaml.Unmarshal([]byte(overrideMemberValues), &actualOverrideMember)
		if err != nil {
			t.Errorf("Failed to unmarshal actual member override values: %v", err)
		}
		whizardEnabled, _, err := unstructured.NestedBool(ccHost, "spec", "monitoring", "whizard", "enabled")
		if err != nil {
			t.Errorf("Failed to get whizard enabled from host cc: %v", err)
		}
		if whizardEnabled {
			extensionValues, err = SetExtensionValues(extensionValues, ccMember, []TransPath{{
				toPath:   "whizard-agent-proxy.config.gatewayUrl",
				fromPath: "spec.monitoring.whizard.client.gatewayUrl",
			}})
			if err != nil {
				t.Errorf("Failed to set whizard gatewayUrl to extension values: %v", err)
			}
		}
		var actualExtension = make(map[string]interface{})
		err = yaml.Unmarshal([]byte(extensionValues), &actualExtension)
		if err != nil {
			t.Errorf("Failed to unmarshal actual extension values: %v", err)
		}

		// get expected
		expectedExtensionValuesBytes, err := os.ReadFile(test.extensionValuesFile)
		if err != nil {
			t.Errorf("Failed to read from %s: %v", test.overrideMemberValuesFile, err)
		}
		var expectedExtension = make(map[string]interface{})
		err = yaml.Unmarshal(expectedExtensionValuesBytes, &expectedExtension)
		if err != nil {
			t.Errorf("Failed to unmarshal expected extension values: %v", err)
		}
		expectedOverrideHostValuesBytes, err := os.ReadFile(test.overrideHostValuesFile)
		if err != nil {
			t.Errorf("Failed to read from %s: %v", test.overrideHostValuesFile, err)
		}
		var expectedOverrideHost = make(map[string]interface{})
		err = yaml.Unmarshal(expectedOverrideHostValuesBytes, &expectedOverrideHost)
		if err != nil {
			t.Errorf("Failed to unmarshal expected host override values: %v", err)
		}
		expectedOverrideMemberValuesBytes, err := os.ReadFile(test.overrideMemberValuesFile)
		if err != nil {
			t.Errorf("Failed to read from %s: %v", test.overrideMemberValuesFile, err)
		}
		var expectedOverrideMember = make(map[string]interface{})
		err = yaml.Unmarshal(expectedOverrideMemberValuesBytes, &expectedOverrideMember)
		if err != nil {
			t.Errorf("Failed to unmarshal expected member  override values: %v", err)
		}

		// compare
		assert.Equal(t, expectedExtension, actualExtension)
		assert.Equal(t, expectedOverrideHost, actualOverrideHost)
		assert.Equal(t, expectedOverrideMember, actualOverrideMember)
	}
}
