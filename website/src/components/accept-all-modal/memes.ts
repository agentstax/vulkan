// What gets pasted over the page when a reader presses Accept all, and
// where each copy lands. Every file is declared once and placed as many
// times as it appears, so a swap is one edit rather than five.

export type MemeImage = {
	source: string;
	// the file's own pixel size, so the width a placement gives it keeps
	// the right aspect
	naturalWidth: number;
	naturalHeight: number;
	// a rectangular still reads as a pasted card and wants the border; art
	// with its own alpha edge would have the frame box in empty pixels
	framed: boolean;
};

export type MemePlacement = {
	image: MemeImage;
	top: string;
	left: string;
	width: number;
	tilt: number;
};

const krabs: MemeImage = {
	source: '/i-like-money.gif',
	naturalWidth: 480,
	naturalHeight: 390,
	framed: true,
};

const laughingPointing: MemeImage = {
	source: '/laughing-pointing.png',
	naturalWidth: 835,
	naturalHeight: 543,
	framed: false,
};

// Tuned by hand rather than scattered at random, so every reader gets the
// same arrangement: the copies ring the box, close enough to crowd it and
// clear enough to leave it readable.
export const memes: MemePlacement[] = [
	{ image: krabs, top: '9%', left: '13%', width: 200, tilt: -9 },
	{ image: krabs, top: '8%', left: '61%', width: 250, tilt: 7 },
	{ image: laughingPointing, top: '22%', left: '32%', width: 180, tilt: 9 },
	{ image: krabs, top: '61%', left: '11%', width: 230, tilt: 6 },
	{ image: krabs, top: '63%', left: '63%', width: 190, tilt: -12 },
	{ image: krabs, top: '76%', left: '39%', width: 150, tilt: 4 },
];
