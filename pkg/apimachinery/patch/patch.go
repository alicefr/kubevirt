/*
 * This file is part of the KubeVirt project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright 2018 Red Hat, Inc.
 *
 */

package patch

import (
	"encoding/json"
	"fmt"
	"strings"
)

type PatchOperation struct {
	Op    PatchOp     `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value"`
}

type PatchOp string

const (
	PatchReplaceOp PatchOp = "replace"
	PatchTestOp    PatchOp = "test"
	PatchAddOp     PatchOp = "add"
	PatchRemoveOp  PatchOp = "remove"
	PatchAnyOp     PatchOp = "any"
)

func (p *PatchOperation) MarshalJSON() ([]byte, error) {
	switch p.Op {
	// The 'remove' operation is the only patching operation without a value
	// and it needs to be parsed differently.
	case PatchRemoveOp:
		return json.Marshal(&struct {
			Op   PatchOp `json:"op"`
			Path string  `json:"path"`
		}{
			Op:   p.Op,
			Path: p.Path,
		})
	case PatchTestOp, PatchReplaceOp, PatchAddOp:
		return json.Marshal(&struct {
			Op    PatchOp     `json:"op"`
			Path  string      `json:"path"`
			Value interface{} `json:"value"`
		}{
			Op:    p.Op,
			Path:  p.Path,
			Value: p.Value,
		})
	default:
		return nil, fmt.Errorf("operation %s not recognized", p.Op)
	}
}

type PatchSet struct {
	patches []PatchOperation
}

type PatchOption func(patches *PatchSet)

func New(opts ...PatchOption) *PatchSet {
	p := &PatchSet{}
	p.AddOption(opts...)
	return p
}

func (p *PatchSet) GetPatches() []PatchOperation {
	return p.patches
}

func (p *PatchSet) AddOption(opts ...PatchOption) {
	for _, f := range opts {
		f(p)
	}
}

func (p *PatchSet) addOp(op PatchOp, path string, value interface{}) {
	p.patches = append(p.patches, PatchOperation{
		Op:    op,
		Path:  path,
		Value: value,
	})
}

func WithTest(path string, value interface{}) PatchOption {
	return func(p *PatchSet) {
		p.addOp(PatchTestOp, path, value)
	}
}

func WithAdd(path string, value interface{}) PatchOption {
	return func(p *PatchSet) {
		p.addOp(PatchAddOp, path, value)
	}
}

func WithReplace(path string, value interface{}) PatchOption {
	return func(p *PatchSet) {
		p.addOp(PatchReplaceOp, path, value)
	}
}

func WithRemove(path string) PatchOption {
	return func(p *PatchSet) {
		p.addOp(PatchRemoveOp, path, nil)
	}
}

func (p *PatchSet) GeneratePayload() ([]byte, error) {
	return GeneratePatchPayload(p.patches...)
}

func (p *PatchSet) IsEmpty() bool {
	return len(p.patches) < 1
}

func GeneratePatchPayload(patches ...PatchOperation) ([]byte, error) {
	if len(patches) == 0 {
		return nil, fmt.Errorf("list of patches is empty")
	}

	payloadBytes, err := json.Marshal(patches)
	if err != nil {
		return nil, err
	}

	return payloadBytes, nil
}

func GenerateTestReplacePatch(path string, oldValue, newValue interface{}) ([]byte, error) {
	return GeneratePatchPayload(
		PatchOperation{
			Op:    PatchTestOp,
			Path:  path,
			Value: oldValue,
		},
		PatchOperation{
			Op:    PatchReplaceOp,
			Path:  path,
			Value: newValue,
		},
	)
}

func UnmarshalPatch(patch []byte, path string, operation *PatchOp, obj any) error {
	var ops []PatchOperation
	if err := json.Unmarshal(patch, &ops); err != nil {
		return err
	}
	for _, op := range ops {
		if operation != nil && op.Op != *operation {
			continue
		}
		if op.Path == path {
			template, err := json.Marshal(op.Value)
			if err != nil {
				return err
			}
			return json.Unmarshal(template, obj)
		}
	}

	return fmt.Errorf("the path %s doesn't exist in the patch", path)
}

func EscapeJSONPointer(ptr string) string {
	s := strings.ReplaceAll(ptr, "~", "~0")
	return strings.ReplaceAll(s, "/", "~1")
}
