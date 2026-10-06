//go:build !linux

package main

import "errors"

func runRouter(config) error { return errors.New("router mode requires Linux") }
func runServer(config) error { return errors.New("server mode requires Linux") }
