// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package cmdrunner

import "log"

type logger struct{}

func (l logger) Debug(msg string) {
	log.Println("D:" + msg)
}

func (l logger) Error(msg string) {
	log.Println("E:" + msg)
}

type noLogger struct{}

func (l noLogger) Debug(msg string) {}
func (l noLogger) Error(msg string) {}
