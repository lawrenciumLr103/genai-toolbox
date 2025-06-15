// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cloudsqlmssql

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/cloudsqlconn"
	"cloud.google.com/go/cloudsqlconn/sqlserver/mssql"
	"github.com/google/uuid"
	"github.com/googleapis/genai-toolbox/tests"
)

var (
	CLOUD_SQL_MSSQL_SOURCE_KIND = "cloud-sql-mssql"
	CLOUD_SQL_MSSQL_TOOL_KIND   = "mssql-sql"
	CLOUD_SQL_MSSQL_PROJECT     = os.Getenv("CLOUD_SQL_MSSQL_PROJECT")
	CLOUD_SQL_MSSQL_REGION      = os.Getenv("CLOUD_SQL_MSSQL_REGION")
	CLOUD_SQL_MSSQL_INSTANCE    = os.Getenv("CLOUD_SQL_MSSQL_INSTANCE")
	CLOUD_SQL_MSSQL_DATABASE    = os.Getenv("CLOUD_SQL_MSSQL_DATABASE")
	CLOUD_SQL_MSSQL_IP          = os.Getenv("CLOUD_SQL_MSSQL_IP")
	CLOUD_SQL_MSSQL_USER        = os.Getenv("CLOUD_SQL_MSSQL_USER")
	CLOUD_SQL_MSSQL_PASS        = os.Getenv("CLOUD_SQL_MSSQL_PASS")
)

func getCloudSQLMssqlVars(t *testing.T) map[string]any {
	switch "" {
	case CLOUD_SQL_MSSQL_PROJECT:
		t.Fatal("'CLOUD_SQL_MSSQL_PROJECT' not set")
	case CLOUD_SQL_MSSQL_REGION:
		t.Fatal("'CLOUD_SQL_MSSQL_REGION' not set")
	case CLOUD_SQL_MSSQL_INSTANCE:
		t.Fatal("'CLOUD_SQL_MSSQL_INSTANCE' not set")
	case CLOUD_SQL_MSSQL_IP:
		t.Fatal("'CLOUD_SQL_MSSQL_IP' not set")
	case CLOUD_SQL_MSSQL_DATABASE:
		t.Fatal("'CLOUD_SQL_MSSQL_DATABASE' not set")
	case CLOUD_SQL_MSSQL_USER:
		t.Fatal("'CLOUD_SQL_MSSQL_USER' not set")
	case CLOUD_SQL_MSSQL_PASS:
		t.Fatal("'CLOUD_SQL_MSSQL_PASS' not set")
	}

	return map[string]any{
		"kind":      CLOUD_SQL_MSSQL_SOURCE_KIND,
		"project":   CLOUD_SQL_MSSQL_PROJECT,
		"instance":  CLOUD_SQL_MSSQL_INSTANCE,
		"ipAddress": CLOUD_SQL_MSSQL_IP,
		"region":    CLOUD_SQL_MSSQL_REGION,
		"database":  CLOUD_SQL_MSSQL_DATABASE,
		"user":      CLOUD_SQL_MSSQL_USER,
		"password":  CLOUD_SQL_MSSQL_PASS,
	}
}

// Copied over from cloud_sql_mssql.go
func initCloudSQLMssqlConnection(project, region, instance, ipAddress, ipType, user, pass, dbname string) (*sql.DB, error) {
	// Create dsn
	dsn := fmt.Sprintf("sqlserver://%s:%s@%s?database=%s&cloudsql=%s:%s:%s", user, pass, ipAddress, dbname, project, region, instance)

	// Get dial options
	dialOpts, err := tests.GetCloudSQLDialOpts(ipType)
	if err != nil {
		return nil, err
	}

	// Register sql server driver
	if !slices.Contains(sql.Drivers(), "cloudsql-sqlserver-driver") {
		_, err := mssql.RegisterDriver("cloudsql-sqlserver-driver", cloudsqlconn.WithDefaultDialOptions(dialOpts...))
		if err != nil {
			return nil, err
		}
	}

	// Open database connection
	db, err := sql.Open(
		"cloudsql-sqlserver-driver",
		dsn,
	)
	if err != nil {
		return nil, err
	}
	return db, nil
}

func TestCloudSQLMssqlToolEndpoints(t *testing.T) {
	sourceConfig := getCloudSQLMssqlVars(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	var args []string

	db, err := initCloudSQLMssqlConnection(CLOUD_SQL_MSSQL_PROJECT, CLOUD_SQL_MSSQL_REGION, CLOUD_SQL_MSSQL_INSTANCE, CLOUD_SQL_MSSQL_IP, "public", CLOUD_SQL_MSSQL_USER, CLOUD_SQL_MSSQL_PASS, CLOUD_SQL_MSSQL_DATABASE)
	if err != nil {
		t.Fatalf("unable to create Cloud SQL connection pool: %s", err)
	}

	// create table name with UUID
	tableNameParam := "param_table_" + strings.ReplaceAll(uuid.New().String(), "-", "")
	tableNameAuth := "auth_table_" + strings.ReplaceAll(uuid.New().String(), "-", "")
	tableNameTemplateParam := "template_param_table_" + strings.ReplaceAll(uuid.New().String(), "-", "")

	// set up data for param tool
	create_statement1, insert_statement1, tool_statement1, params1 := tests.GetMssqlParamToolInfo(tableNameParam)
	teardownTable1 := tests.SetupMsSQLTable(t, ctx, db, create_statement1, insert_statement1, tableNameParam, params1)
	defer teardownTable1(t)

	// set up data for auth tool
	create_statement2, insert_statement2, tool_statement2, params2 := tests.GetMssqlAuthToolInfo(tableNameAuth)
	teardownTable2 := tests.SetupMsSQLTable(t, ctx, db, create_statement2, insert_statement2, tableNameAuth, params2)
	defer teardownTable2(t)

	// Write config into a file and pass it to command
	toolsFile := tests.GetToolsConfig(sourceConfig, CLOUD_SQL_MSSQL_TOOL_KIND, tool_statement1, tool_statement2)
	toolsFile = tests.AddMssqlExecuteSqlConfig(t, toolsFile)
	tmplSelectCombined, tmplSelectFilterCombined := tests.GetMssqlTmplToolStatement()
	toolsFile = tests.AddTemplateParamConfig(t, toolsFile, CLOUD_SQL_MSSQL_TOOL_KIND, tmplSelectCombined, tmplSelectFilterCombined)

	cmd, cleanup, err := tests.StartCmd(ctx, toolsFile, args...)
	if err != nil {
		t.Fatalf("command initialization returned an error: %s", err)
	}
	defer cleanup()

	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := cmd.WaitForString(waitCtx, regexp.MustCompile(`Server ready to serve`))
	if err != nil {
		t.Logf("toolbox command logs: \n%s", out)
		t.Fatalf("toolbox didn't start successfully: %s", err)
	}

	tests.RunToolGetTest(t)

	select1Want, failInvocationWant, createTableStatement := tests.GetMssqlWants()
	invokeParamWant, mcpInvokeParamWant, tmplSelectAllWant, tmplSelect1Want := tests.GetNonSpannerInvokeParamWant()
	tests.RunToolInvokeTest(t, select1Want, invokeParamWant)
	tests.RunExecuteSqlToolInvokeTest(t, createTableStatement, select1Want)
	tests.RunMCPToolCallMethod(t, mcpInvokeParamWant, failInvocationWant)
	tests.RunToolInvokeWithTemplateParameters(t, tableNameTemplateParam, tmplSelectAllWant, tmplSelect1Want, "", "", false, false)
}

// Test connection with different IP type
func TestCloudSQLMssqlIpConnection(t *testing.T) {
	sourceConfig := getCloudSQLMssqlVars(t)

	tcs := []struct {
		name   string
		ipType string
	}{
		{
			name:   "public ip",
			ipType: "public",
		},
		{
			name:   "private ip",
			ipType: "private",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			sourceConfig["ipType"] = tc.ipType
			err := tests.RunSourceConnectionTest(t, sourceConfig, CLOUD_SQL_MSSQL_TOOL_KIND)
			if err != nil {
				t.Fatalf("Connection test failure: %s", err)
			}
		})
	}
}
