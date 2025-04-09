package v2beta2

import (
	"fmt"
	"regexp"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

type LabelSelector struct {
	// matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels
	// map is equivalent to an element of matchExpressions, whose key field is "key", the
	// operator is "In", and the values array contains only "value". The requirements are ANDed.
	// +optional
	MatchLabels map[string]string `json:"matchLabels,omitempty" protobuf:"bytes,1,rep,name=matchLabels"`
	// matchExpressions is a list of label selector requirements. The requirements are ANDed.
	// +optional
	MatchExpressions []LabelSelectorRequirement `json:"matchExpressions,omitempty" protobuf:"bytes,2,rep,name=matchExpressions"`
}

type LabelSelectorRequirement struct {
	// key is the label key that the selector applies to.
	// +patchMergeKey=key
	// +patchStrategy=merge
	Key string `json:"key" patchStrategy:"merge" patchMergeKey:"key" protobuf:"bytes,1,opt,name=key"`
	// operator represents a key's relationship to a set of values.
	// Valid operators are In, NotIn, Exists and DoesNotExist.
	Operator LabelSelectorOperator `json:"operator" protobuf:"bytes,2,opt,name=operator,casttype=LabelSelectorOperator"`
	// values is an array of string values. If the operator is In or NotIn,
	// the values array must be non-empty. If the operator is Exists or DoesNotExist,
	// the values array must be empty. This array is replaced during a strategic
	// merge patch.
	// +optional
	Values     []string `json:"values,omitempty" protobuf:"bytes,3,rep,name=values"`
	RegexValue string   `json:"regexValue,omitempty" protobuf:"bytes,3,rep,name=regexValue"`
}

// LabelSelectorOperator is a label selector operator is the set of operators that can be used in a selector requirement.
type LabelSelectorOperator string

const (
	LabelSelectorOpMatch LabelSelectorOperator = "Match"
)

func (ls *LabelSelector) Matches(label map[string]string) (bool, error) {
	if label == nil {
		return false, nil
	}

	selector := &metav1.LabelSelector{
		MatchLabels: ls.MatchLabels,
	}
	for _, requirement := range ls.MatchExpressions {
		if requirement.Operator != LabelSelectorOpMatch {
			selector.MatchExpressions = append(selector.MatchExpressions, metav1.LabelSelectorRequirement{
				Key:      requirement.Key,
				Operator: metav1.LabelSelectorOperator(requirement.Operator),
				Values:   requirement.Values,
			})
		} else {
			match, err := regexp.MatchString(requirement.RegexValue, label[requirement.Key])
			if !match {
				return false, err
			}
		}
	}

	sl, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return false, err
	}

	return sl.Matches(labels.Set(label)), nil
}

func (ls *LabelSelector) Validate() error {
	selector := &metav1.LabelSelector{
		MatchLabels: ls.MatchLabels,
	}

	for _, requirement := range ls.MatchExpressions {
		if requirement.Operator != LabelSelectorOpMatch {
			selector.MatchExpressions = append(selector.MatchExpressions, metav1.LabelSelectorRequirement{
				Key:      requirement.Key,
				Operator: metav1.LabelSelectorOperator(requirement.Operator),
				Values:   requirement.Values,
			})
		} else {
			_, err := regexp.Compile(requirement.RegexValue)
			if err != nil {
				return err
			}
		}
	}

	_, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return err
	}

	return nil
}

func LabelMatchSelector(label map[string]string, selector *LabelSelector) bool {

	if selector == nil {
		return true
	}
	ok, err := selector.Matches(label)
	if err != nil {
		return false
	}

	return ok
}

type ValueSource struct {
	// Selects a key of a secret in the pod's namespace
	// +optional
	SecretKeyRef *SecretKeySelector `json:"secretKeyRef,omitempty" protobuf:"bytes,4,opt,name=secretKeyRef"`
}

type Credential struct {
	// +optional
	Value     string       `json:"value,omitempty" protobuf:"bytes,2,opt,name=value"`
	ValueFrom *ValueSource `json:"valueFrom,omitempty" protobuf:"bytes,3,opt,name=valueFrom"`
}

func (c *Credential) ToString() string {
	if len(c.Value) > 0 {
		return c.Value
	}

	if c.ValueFrom != nil {
		if c.ValueFrom.SecretKeyRef != nil {
			return fmt.Sprintf("%s/%s/%s", c.ValueFrom.SecretKeyRef.Namespace, c.ValueFrom.SecretKeyRef.Name, c.ValueFrom.SecretKeyRef.Key)
		}
	}

	return ""
}
