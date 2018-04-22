#!/bin/bash
go get github.com/mitchellh/gox
go get github.com/tcnksm/ghr

export APPNAME="nats-top"
export OSARCH="linux/amd64 darwin/amd64 linux/arm windows/amd64"
export DIRS="linux_amd64 darwin_amd64 linux_arm windows_amd64"
export OUTDIR="pkg"

# NOTE: Hack for avoiding issues when cross compiling.
rm $HOME/gopath/src/github.com/nats-io/gnatsd/server/pse_*.go
cat <<HERE > $HOME/gopath/src/github.com/nats-io/gnatsd/server/pse.go
// Copyright 2016-2018 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This is a placeholder for now.
func procUsage(pcpu *float64, rss, vss *int64) error {
	*pcpu = 0.0
	*rss = 0
	*vss = 0
	return nil
}
HERE

gox -osarch="$OSARCH" -output "$OUTDIR/$APPNAME-{{.OS}}_{{.Arch}}/$APPNAME"
for dir in $DIRS; do \
	(cp readme.md $OUTDIR/$APPNAME-$dir/readme.md) ;\
	(cp LICENSE $OUTDIR/$APPNAME-$dir/LICENSE) ;\
	(cd $OUTDIR && zip -q $APPNAME-$dir.zip -r $APPNAME-$dir) ;\
	echo "created '$OUTDIR/$APPNAME-$dir.zip', cleaning up..." ;\
	rm -rf $APPNAME-$dir;\
done
