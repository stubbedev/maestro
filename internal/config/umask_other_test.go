//go:build !unix

package config

func setUmask(int) func() { return func() {} }
