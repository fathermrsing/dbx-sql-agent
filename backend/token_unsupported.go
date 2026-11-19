//go:build !darwin || !cgo

package main

import "errors"

type unavailableVault struct{}

func newTokenVault() tokenVault                     { return unavailableVault{} }
func (unavailableVault) supported() bool            { return false }
func (unavailableVault) has(string) (bool, error)   { return false, nil }
func (unavailableVault) get(string) (string, error) { return "", nil }
func (unavailableVault) set(string, string) error   { return errors.New("系统凭据存储不可用") }
func (unavailableVault) delete(string) error        { return errors.New("系统凭据存储不可用") }
