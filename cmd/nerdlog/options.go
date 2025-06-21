package main

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/dimonomid/nerdlog/core"
	"github.com/juju/errors"
)

type Options struct {
	Timezone *time.Location

	// MaxNumLines is how many log lines the nerdlog_agent.sh will return at
	// most. Initially it's set to 250.
	MaxNumLines int

	DefaultTransportMode *core.TransportMode
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

func (o *OptionsShared) GetTransportMode() *core.TransportMode {
	o.mtx.Lock()
	defer o.mtx.Unlock()
	return o.options.DefaultTransportMode
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
		Help: "How many log lines to fetch from each logstream in one query",
	},
	"numlines": {
		AliasOf: "maxnumlines",
	}, // }}}
	"transport": { // {{{
		Get: func(o *Options) string {
			return o.DefaultTransportMode.String()
		},
		Set: func(o *Options, value string) error {
			tm, err := core.ParseTransportMode(value)
			if err != nil {
				return errors.Trace(err)
			}

			o.DefaultTransportMode = tm
			return nil
		},
		Help: "How to connect to remote hosts",
	}, // }}}
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
