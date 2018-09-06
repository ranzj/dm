// Copyright 2018 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

// Package config contains configurations for Task, SubTask, etc.
// Make config as a package to break the dependence between components and units.
package config

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/juju/errors"
	"github.com/ngaut/log"
	"github.com/pingcap/tidb-enterprise-tools/pkg/filter"
	bf "github.com/pingcap/tidb-tools/pkg/binlog-filter"
	column "github.com/pingcap/tidb-tools/pkg/column-mapping"
	"github.com/pingcap/tidb-tools/pkg/table-router"
	"github.com/siddontang/go-mysql/mysql"
)

// task modes
const (
	ModeAll       = "all"
	ModeFull      = "full"
	ModeIncrement = "incremental"
)

// DBConfig is the DB configuration.
type DBConfig struct {
	Host     string `toml:"host" json:"host" yaml:"host"`
	Port     int    `toml:"port" json:"port" yaml:"port"`
	User     string `toml:"user" json:"user" yaml:"user"`
	Password string `toml:"password" json:"-" yaml:"password"` // omit it for privacy
}

// SubTaskConfig is the configuration for SubTask
type SubTaskConfig struct {
	// BurntSushi/toml seems have a bug for flag "-"
	// when doing encoding, if we use `toml:"-"`, it still try to encode it
	// and it will panic because of unsupported type (reflect.Func)
	// so we should not export flagSet
	flagSet *flag.FlagSet

	Name           string `toml:"name" json:"name"`
	Mode           string `toml:"mode" json:"mode"`
	ServerID       int    `toml:"server-id" json:"server-id"`
	Flavor         string `toml:"flavor" json:"flavor"`
	VerifyChecksum bool   `toml:"verify-checksum" json:"verify-checksum"`

	BinlogType string   `toml:"binlog-type" json:"binlog-type"`
	RelayDir   string   `toml:"relay-dir" json:"relay-dir"`
	From       DBConfig `toml:"from" json:"from"`
	To         DBConfig `toml:"to" json:"to"`

	RouteRules         []*router.TableRule   `toml:"route-rules" json:"route-rules"`
	FilterRules        []*bf.BinlogEventRule `toml:"filter-rules" json:"filter-rules"`
	ColumnMappingRules []*column.Rule        `toml:"mapping-rule" json:"mapping-rule"`
	BWList             *filter.Rules         `toml:"black-white-list" json:"black-white-list"`

	MydumperConfig // Mydumper configuration
	LoaderConfig   // Loader configuration
	SyncerConfig   // Syncer configuration

	// compatible with standalone dm unit
	LogLevel  string `toml:"log-level" json:"log-level"`
	LogFile   string `toml:"log-file" json:"log-file"`
	LogRotate string `toml:"log-rotate" json:"log-rotate"`

	PprofAddr  string `toml:"pprof-addr" json:"pprof-addr"`
	StatusAddr string `toml:"status-addr" json:"status-addr"`

	ConfigFile string `toml:"-" json:"config-file"`
}

// NewSubTaskConfig creates a new SubTaskConfig
func NewSubTaskConfig() *SubTaskConfig {
	cfg := &SubTaskConfig{}

	cfg.flagSet = flag.NewFlagSet("subtask", flag.ContinueOnError)
	fs := cfg.flagSet

	// Loader configuration
	fs.IntVar(&cfg.PoolSize, "t", 16, "Number of threads restoring concurrently for worker pool. Each worker restore one file at a time, increase this as TiKV nodes increase")
	fs.StringVar(&cfg.Dir, "d", "./", "Directory of the dump to import")
	fs.StringVar(&cfg.CheckPointSchema, "checkpoint-schema", "tidb_loader", "schema name of checkpoint")
	fs.BoolVar(&cfg.RemoveCheckpoint, "rm-checkpoint", false, "delete corresponding checkpoint records after the table is restored successfully")

	// Syncer configuration
	fs.IntVar(&cfg.ServerID, "server-id", 101, "MySQL slave server ID")
	fs.StringVar(&cfg.Meta, "meta", "", "syncer meta info") // maybe refine to use target DB save checkpoint
	fs.StringVar(&cfg.Flavor, "flavor", mysql.MySQLFlavor, "use flavor for different MySQL source versions; support \"mysql\", \"mariadb\" now; if you replicate from mariadb, please set it to \"mariadb\"")
	fs.IntVar(&cfg.WorkerCount, "count", 16, "parallel worker count")
	fs.IntVar(&cfg.Batch, "b", 10, "batch commit count")
	fs.IntVar(&cfg.MaxRetry, "max-retry", 100, "maxinum retry when network interruption")
	fs.BoolVar(&cfg.EnableGTID, "enable-gtid", false, "enable gtid mode")
	fs.BoolVar(&cfg.AutoFixGTID, "auto-fix-gtid", false, "auto fix gtid while switch mysql master/slave")
	fs.BoolVar(&cfg.DisableCausality, "disable-detect", false, "disbale detect causality")
	fs.BoolVar(&cfg.SafeMode, "safe-mode", false, "enable safe mode to make syncer reentrant")

	// compatible with standalone dm unit
	fs.StringVar(&cfg.PprofAddr, "pprof-addr", ":10084", "Loader pprof addr")
	fs.StringVar(&cfg.StatusAddr, "status-addr", "", "Syncer status addr")
	fs.StringVar(&cfg.LogLevel, "L", "info", "sub task log level: debug, info, warn, error, fatal")
	fs.StringVar(&cfg.LogFile, "log-file", "", "log file path")
	fs.StringVar(&cfg.LogRotate, "log-rotate", "day", "log file rotate type, hour/day")

	fs.StringVar(&cfg.ConfigFile, "config", "", "config file")

	return cfg
}

// MySQLInstanceID returns the relevant MySQL instance ID of config
func (c *SubTaskConfig) MySQLInstanceID() string {
	return fmt.Sprintf("%s:%d", c.From.Host, c.From.Port)
}

// String returns the config's json string
func (c *SubTaskConfig) String() string {
	cfg, err := json.Marshal(c)
	if err != nil {
		log.Errorf("[config] marshal sub task dm config to json error %v", err)
	}
	return string(cfg)
}

// Toml returns TOML format representation of config
func (c *SubTaskConfig) Toml() (string, error) {
	var b bytes.Buffer
	enc := toml.NewEncoder(&b)
	err := enc.Encode(c)
	if err != nil {
		return "", errors.Trace(err)
	}
	return b.String(), nil
}

// DecodeFile loads and decodes config from file
func (c *SubTaskConfig) DecodeFile(fpath string) error {
	_, err := toml.DecodeFile(fpath, c)
	if err != nil {
		return errors.Trace(err)
	}

	return errors.Trace(c.adjust())
}

// Decode loads config from file data
func (c *SubTaskConfig) Decode(data string) error {
	_, err := toml.Decode(data, c)
	if err != nil {
		return errors.Trace(err)
	}

	return errors.Trace(c.adjust())
}

// adjust adjusts configs
func (c *SubTaskConfig) adjust() error {
	if c.Flavor != mysql.MySQLFlavor && c.Flavor != mysql.MariaDBFlavor {
		return errors.Errorf("please specify right mysql version, support mysql, mariadb now")
	}

	c.BWList.ToLower()

	// add ToLower for other pkg later
	for _, rule := range c.RouteRules {
		rule.SchemaPattern = strings.ToLower(rule.SchemaPattern)
		rule.TablePattern = strings.ToLower(rule.TablePattern)
	}

	for _, rule := range c.FilterRules {
		rule.SchemaPattern = strings.ToLower(rule.SchemaPattern)
		rule.TablePattern = strings.ToLower(rule.TablePattern)
	}

	for _, rule := range c.ColumnMappingRules {
		rule.PatternSchema = strings.ToLower(rule.PatternSchema)
		rule.PatternTable = strings.ToLower(rule.PatternTable)
	}

	if c.MaxRetry == 0 {
		c.MaxRetry = 1
	}

	return nil
}

// Parse parses flag definitions from the argument list.
func (c *SubTaskConfig) Parse(arguments []string) error {
	// Parse first to get config file.
	err := c.flagSet.Parse(arguments)
	if err != nil {
		return errors.Trace(err)
	}

	// Load config file if specified.
	if c.ConfigFile != "" {
		err = c.DecodeFile(c.ConfigFile)
		if err != nil {
			return errors.Trace(err)
		}
	}

	// Parse again to replace with command line options.
	err = c.flagSet.Parse(arguments)
	if err != nil {
		return errors.Trace(err)
	}

	if len(c.flagSet.Args()) != 0 {
		return errors.Errorf("'%s' is an invalid flag", c.flagSet.Arg(0))
	}

	return errors.Trace(c.adjust())
}
