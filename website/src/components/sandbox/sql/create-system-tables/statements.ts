// the statement order of createSystemTables -- one entry per Exec in the Go
// method. Both lists below walk that order: the templates as the Go source
// writes them, which the drift test reads, and the statements with the schema
// filled, which the sandbox runs. Keeping them in one file is what makes a
// statement added to only one of them visible.
import { interpolate } from '../interpolate';
import { createSystemConfigSql } from './create-system-config';
import { createTopicConfigSql } from './create-topic-config';
import { createTopicConfigLogSql } from './create-topic-config-log';
import { createTopicConfigLogIndexSql } from './create-topic-config-log-index';
import { createConsumerGroupConfigSql } from './create-consumer-group-config';
import { createWorkerConfigSql } from './create-worker-config';
import { createWorkerConfigTopicNameIndexSql } from './create-worker-config-topic-name-index';
import { createWorkerConfigGroupNameIndexSql } from './create-worker-config-group-name-index';
import { createWorkerConfigSystemNameIndexSql } from './create-worker-config-system-name-index';
import { createWorkerConfigLogSql } from './create-worker-config-log';
import { createWorkerConfigLogIndexSql } from './create-worker-config-log-index';
import { createWorkerInstanceSql } from './create-worker-instance';
import { createWorkerInstanceWorkerIndexSql } from './create-worker-instance-worker-index';
import { createWorkerInstanceExpiryIndexSql } from './create-worker-instance-expiry-index';
import { createScheduleConfigSql } from './create-schedule-config';
import { createScheduleCursorSql } from './create-schedule-cursor';
import { createScheduleCursorDueIndexSql } from './create-schedule-cursor-due-index';
import { createMigrationLogSql } from './create-migration-log';

export const createSystemTablesTemplates: string[] = [
	createSystemConfigSql,
	createTopicConfigSql,
	createTopicConfigLogSql,
	createTopicConfigLogIndexSql,
	createConsumerGroupConfigSql,
	createWorkerConfigSql,
	createWorkerConfigTopicNameIndexSql,
	createWorkerConfigGroupNameIndexSql,
	createWorkerConfigSystemNameIndexSql,
	createWorkerConfigLogSql,
	createWorkerConfigLogIndexSql,
	createWorkerInstanceSql,
	createWorkerInstanceWorkerIndexSql,
	createWorkerInstanceExpiryIndexSql,
	createScheduleConfigSql,
	createScheduleCursorSql,
	createScheduleCursorDueIndexSql,
	createMigrationLogSql,
];

// every statement names only shared tables, so the schema is the one verb to
// fill -- the topic side's sibling takes the topic id as well
export function createSystemTablesStatements(): string[] {
	return createSystemTablesTemplates.map((template) => interpolate(template));
}
