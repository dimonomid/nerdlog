package main

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/juju/errors"
)

type Options struct {
	Timezone *time.Location

	// MaxNumLines is how many log lines the nerdlog_agent.sh will return at
	// most. Initially it's set to 250.
	MaxNumLines int

	// EphemeralKeyProvider specifies which ephemeral key provider to use.
	// Valid values: "mock", "opkssh", or empty string to disable.
	EphemeralKeyProvider string
}

type OptionsShared struct {
	mtx     *sync.Mutex
	options Options
}

func NewOptionsShared(options Options) *OptionsShared {
	return &OptionsShared{
		mtx:     &sync.Mutex{},
		options: options,
	}
}

func (o *OptionsShared) GetTimezone() *time.Location {
	o.mtx.Lock()
	defer o.mtx.Unlock()
	return o.options.Timezone
}

func (o *OptionsShared) GetMaxNumLines() int {
	o.mtx.Lock()
	defer o.mtx.Unlock()
	return o.options.MaxNumLines
}

func (o *OptionsShared) GetEphemeralKeyProvider() string {
	o.mtx.Lock()
	defer o.mtx.Unlock()
	return o.options.EphemeralKeyProvider
}

func (o *OptionsShared) GetAll() Options {
	o.mtx.Lock()
	defer o.mtx.Unlock()
	return o.options
}

func (o *OptionsShared) Call(f func(o *Options)) {
	o.mtx.Lock()
	defer o.mtx.Unlock()
	f(&o.options)
}

type OptionMeta struct {
	// If AliasOf is non-empty, all the other fields are ignored.
	AliasOf string

	Get  func(o *Options) string
	Set  func(o *Options, value string) error
	Help string
}

var AllOptions = map[string]*OptionMeta{
	"timezone": { // {{{
		Get: func(o *Options) string {
			return o.Timezone.String()
		},
		Set: func(o *Options, value string) error {
			loc, err := time.LoadLocation(value)
			if err != nil {
				return errors.Trace(err)
			}

			o.Timezone = loc
			return nil
		},
		Help: "Timezone to use in the UI.",
	}, // }}}
	"maxnumlines": { // {{{
		Get: func(o *Options) string {
			return fmt.Sprint(o.MaxNumLines)
		},
		Set: func(o *Options, value string) error {
			maxNumLines, err := strconv.Atoi(value)
			if err != nil {
				return errors.Trace(err)
			}

			if maxNumLines < 2 {
				return errors.Errorf("numlines must be at least 2")
			}

			o.MaxNumLines = maxNumLines
			return nil
		},
		Help: "How many log messages to fetch from each logstream in one query",
	},
	"numlines": {
		AliasOf: "maxnumlines",
	}, // }}}
	"ephemeralkeyprovider": {
		Get: func(o *Options) string {
			return o.EphemeralKeyProvider
		},
		Set: func(o *Options, value string) error {
			switch value {
			case "", "mock", "opkssh":
				o.EphemeralKeyProvider = value
			default:
				return errors.Errorf("invalid ephemeral key provider: %s (valid values: mock, opkssh, or empty to disable)", value)
			}
			return nil
		},
		Help: "Ephemeral SSH key provider to use (mock, opkssh, or empty to disable)",
	},
}

func OptionMetaByName(name string) *OptionMeta {
	meta, ok := AllOptions[name]
	if !ok {
		return nil
	}

	if meta.AliasOf != "" {
		var ok bool
		meta, ok = AllOptions[meta.AliasOf]
		if !ok {
			// This one would mean a programmer error, so we panic here.
			panic(fmt.Sprintf("option %s is defined as an alias of non-existing option %s", name, meta.AliasOf))
		}
	}

	if meta.AliasOf != "" {
		panic(fmt.Sprintf("option %s is defined as an alias of another alias %s", name, meta.AliasOf))
	}

	return meta
}
