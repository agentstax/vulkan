import { execSync } from 'node:child_process';
import { statSync } from 'node:fs';

// An uncommitted file has no commit yet; its filesystem modification
// date is the real fact available.
export function lastCommitDate(filePath: string): string {
	const committed = execSync(`git log -1 --format=%cs -- "${filePath}"`, {
		encoding: 'utf8',
	}).trim();
	if (committed !== '') {
		return committed;
	}

	return statSync(filePath).mtime.toISOString().slice(0, 10);
}
