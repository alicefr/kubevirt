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
 * Copyright 2022 Red Hat, Inc.
 *
 */

package cache

import (
	"path/filepath"
	"strconv"

	v1 "kubevirt.io/api/core/v1"

	"kubevirt.io/kubevirt/pkg/util"
)

type PodIfaceState int

const (
	PodIfaceNetworkPreparationPending PodIfaceState = iota
	PodIfaceNetworkPreparationStarted
	PodIfaceNetworkPreparationFinished
)

type PodIfaceCacheData struct {
	Iface  *v1.Interface `json:"iface,omitempty"`
	PodIP  string        `json:"podIP,omitempty"`
	PodIPs []string      `json:"podIPs,omitempty"`
	State  PodIfaceState `json:"networkState,omitempty"`
}

type PodInterfaceCache struct {
	cache *Cache
}

func ReadPodInterfaceCache(c cacheCreator, uid, ifaceName string, pid int) (*PodIfaceCacheData, error) {
	podCache, err := NewPodInterfaceCache(c, uid, pid).IfaceEntry(ifaceName)
	if err != nil {
		return nil, err
	}
	return podCache.Read()
}

func WritePodInterfaceCache(c cacheCreator, uid, ifaceName string, pid int, cacheInterface *PodIfaceCacheData) error {
	podCache, err := NewPodInterfaceCache(c, uid, pid).IfaceEntry(ifaceName)
	if err != nil {
		return err
	}
	return podCache.Write(cacheInterface)
}

func DeletePodInterfaceCache(c cacheCreator, uid, ifaceName string, pid int) error {
	podCache, err := NewPodInterfaceCache(c, uid, pid).IfaceEntry(ifaceName)
	if err != nil {
		return err
	}
	return podCache.Remove()
}

func NewPodInterfaceCache(creator cacheCreator, uid string, pid int) PodInterfaceCache {
	const podIfaceCacheDirName = "network-info-cache"
	return PodInterfaceCache{creator.New(filepath.Join(util.VirtPrivateDir, podIfaceCacheDirName, uid, strconv.Itoa(pid)))}
}

func (p PodInterfaceCache) IfaceEntry(ifaceName string) (PodInterfaceCache, error) {
	cache, err := p.cache.Entry(ifaceName)
	if err != nil {
		return PodInterfaceCache{}, err
	}

	return PodInterfaceCache{&cache}, nil
}

func (p PodInterfaceCache) Read() (*PodIfaceCacheData, error) {
	iface := &PodIfaceCacheData{}
	_, err := p.cache.Read(iface)
	return iface, err
}

func (p PodInterfaceCache) Write(cacheInterface *PodIfaceCacheData) error {
	return p.cache.Write(cacheInterface)
}

func (p PodInterfaceCache) Remove() error {
	return p.cache.Delete()
}
