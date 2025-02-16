/*
Copyright 2020 The Vitess Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package repltracker

import (
	"sync"
	"time"

	"vitess.io/vitess/go/stats"

	"vitess.io/vitess/go/vt/mysqlctl"
	vtrpcpb "vitess.io/vitess/go/vt/proto/vtrpc"
	"vitess.io/vitess/go/vt/vterrors"
)

var replicationLagSeconds = stats.NewGauge("replicationLagSec", "replication lag in seconds")

type poller struct {
	mysqld mysqlctl.MysqlDaemon

	mu           sync.Mutex
	lag          time.Duration
	timeRecorded time.Time
}

func (p *poller) InitDBConfig(mysqld mysqlctl.MysqlDaemon) {
	p.mysqld = mysqld
}

func (p *poller) Status() (time.Duration, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	status, err := p.mysqld.ReplicationStatus()
	if err != nil {
		return 0, err
	}

	// If replication is not currently running or we don't know what the lag is -- most commonly
	// because the replica mysqld is in the process of trying to start replicating from its source
	// but it hasn't yet reached the point where it can calculate the seconds_behind_master
	// value and it's thus NULL -- then we will estimate the lag ourselves using the last seen
	// value + the time elapsed since.
	if !status.ReplicationRunning() || status.ReplicationLagUnknown {
		if p.timeRecorded.IsZero() {
			return 0, vterrors.Errorf(vtrpcpb.Code_UNAVAILABLE, "replication is not running")
		}
		return time.Since(p.timeRecorded) + p.lag, nil
	}

	p.lag = time.Duration(status.ReplicationLagSeconds) * time.Second
	p.timeRecorded = time.Now()
	replicationLagSeconds.Set(int64(p.lag.Seconds()))
	return p.lag, nil
}
