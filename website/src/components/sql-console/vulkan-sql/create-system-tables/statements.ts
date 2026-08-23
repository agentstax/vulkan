// the statement order of createSystemTables -- one entry per Exec in the Go method
import { createSystemSql } from './create-system';
import { createTopicSql } from './create-topic';
import { createTopicLogSql } from './create-topic-log';
import { createTopicLogIndexSql } from './create-topic-log-index';
import { createConsumerGroupSql } from './create-consumer-group';
import { createWorkerSql } from './create-worker';
import { createWorkerTopicNameIndexSql } from './create-worker-topic-name-index';
import { createWorkerGroupNameIndexSql } from './create-worker-group-name-index';
import { createWorkerSystemNameIndexSql } from './create-worker-system-name-index';
import { createWorkerLogSql } from './create-worker-log';
import { createWorkerLogIndexSql } from './create-worker-log-index';
import { createWorkerInstanceSql } from './create-worker-instance';
import { createWorkerInstanceWorkerIndexSql } from './create-worker-instance-worker-index';
import { createWorkerInstanceExpiryIndexSql } from './create-worker-instance-expiry-index';
import { createCronJobSql } from './create-cron-job';
import { createCronJobDueIndexSql } from './create-cron-job-due-index';
import { createMigrationLogSql } from './create-migration-log';

export const createSystemTablesStatements: string[] = [
	createSystemSql,
	createTopicSql,
	createTopicLogSql,
	createTopicLogIndexSql,
	createConsumerGroupSql,
	createWorkerSql,
	createWorkerTopicNameIndexSql,
	createWorkerGroupNameIndexSql,
	createWorkerSystemNameIndexSql,
	createWorkerLogSql,
	createWorkerLogIndexSql,
	createWorkerInstanceSql,
	createWorkerInstanceWorkerIndexSql,
	createWorkerInstanceExpiryIndexSql,
	createCronJobSql,
	createCronJobDueIndexSql,
	createMigrationLogSql,
];
