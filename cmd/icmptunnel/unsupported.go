//go:build !linux

package main

import "errors"

func runRouter(config) error                        { return errors.New("router mode requires Linux") }
func runRouterStatus(config, *statusReporter) error { return errors.New("router mode requires Linux") }
func runServer(config) error                        { return errors.New("server mode requires Linux") }
func runIP([]string) error                          { return errors.New("routing commands require Linux") }
