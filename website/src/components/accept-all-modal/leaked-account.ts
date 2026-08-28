// The numbers the modal dumps on screen. They are noise, generated fresh
// every time the modal opens and belonging to nobody -- what matters is
// that the SHAPE is right, because a number of the wrong length reads as a
// prop and the joke needs the half-second where it does not.

// A routing number's first two digits name a Federal Reserve district, so
// only these ranges produce one a reader would recognize as real.
const routingPrefixRanges: [[number, number], ...[number, number][]] = [
	[1, 12],
	[21, 32],
	[61, 72],
	[80, 80],
];

// the ABA position weights, which the ninth digit then closes
const routingWeights = [3, 7, 1, 3, 7, 1, 3, 7];

// US account numbers run 8 to 12 digits and carry no checksum; the top of
// that range reads as the most alarming
const accountDigits = 12;

export type LeakedAccount = {
	routingNumber: string;
	accountNumber: string;
};

export function newLeakedAccount(): LeakedAccount {
	return {
		routingNumber: newRoutingNumber(),
		accountNumber: newAccountNumber(),
	};
}

// ***************
// *** HELPERS ***
// ***************

function newRoutingNumber(): string {
	const digits = newRoutingPrefix();
	while (digits.length < routingWeights.length) {
		digits.push(randomDigit());
	}

	digits.push(routingCheckDigit(digits));
	return digits.join('');
}

function newRoutingPrefix(): number[] {
	const index = randomBelow(routingPrefixRanges.length);
	const [low, high] = routingPrefixRanges[index] ?? routingPrefixRanges[0];
	const prefix = low + randomBelow(high - low + 1);
	return [Math.floor(prefix / 10), prefix % 10];
}

// 3(d1+d4+d7) + 7(d2+d5+d8) + (d3+d6+d9) has to end in zero, and the ninth
// digit carries weight one, so it is whatever closes the sum
function routingCheckDigit(first: number[]): number {
	let sum = 0;
	for (let index = 0; index < routingWeights.length; index++) {
		sum += (routingWeights[index] ?? 0) * (first[index] ?? 0);
	}

	return (10 - (sum % 10)) % 10;
}

function newAccountNumber(): string {
	let digits = '';
	while (digits.length < accountDigits) {
		digits += String(randomDigit());
	}

	return digits;
}

function randomDigit(): number {
	return randomBelow(10);
}

function randomBelow(bound: number): number {
	return Math.floor(Math.random() * bound);
}
