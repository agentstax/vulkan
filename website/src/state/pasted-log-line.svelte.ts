// The log line a reader pasted on a code thread. Deliberately not stored: it
// carries their own topic names and error text, and nothing needs it after the
// tab closes.

export class PastedLogLine {
	text = $state('');

	set(text: string): void {
		this.text = text;
	}

	clear(): void {
		this.text = '';
	}
}

export const pastedLogLine = new PastedLogLine();
