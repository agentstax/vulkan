import { execSync } from 'node:child_process';
import { statSync } from 'node:fs';
import { relative, resolve } from 'node:path';

type CommitLog = {
	root: string;
	dates: Map<string, string>;
	oldestDate: string;
};

// one walk of the whole history, cached for the process: the boards ask
// for hundreds of files, and a git process per file left the dev
// server's first page hanging for the length of the queue
let commitLog: CommitLog | undefined;

// An uncommitted file has no commit yet; its filesystem modification
// date is the real fact available.
export function lastCommitDate(filePath: string): string {
	const log = loadCommitLog();
	const committed = log.dates.get(relative(log.root, resolve(filePath)));
	if (committed !== undefined) {
		return committed;
	}

	return statSync(filePath).mtime.toISOString().slice(0, 10);
}

export function firstCommitDate(): string {
	return loadCommitLog().oldestDate;
}

// ***************
// *** HELPERS ***
// ***************

function loadCommitLog(): CommitLog {
	if (commitLog !== undefined) {
		return commitLog;
	}

	const root = execSync('git rev-parse --show-toplevel', { encoding: 'utf8' }).trim();
	// newest first; each commit prints its date NUL-prefixed so it never
	// reads as a file name, so a path's first date is its newest commit
	const walk = execSync('git log --format=%x00%cs --name-only', {
		encoding: 'utf8',
		maxBuffer: 64 * 1024 * 1024,
	});

	const dates = new Map<string, string>();
	let date = '';
	for (const line of walk.split('\n')) {
		if (line.startsWith('\0')) {
			date = line.slice(1);
		} else if (line !== '' && !dates.has(line)) {
			dates.set(line, date);
		}
	}

	// the walk is newest-first, so the date left standing is the first commit's
	commitLog = { root, dates, oldestDate: date };
	return commitLog;
}
