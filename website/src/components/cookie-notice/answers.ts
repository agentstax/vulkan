import type { Answer } from '../cookie-answer/types';

// Every control in act one has its own answer: which one the reader
// pressed picks both where the answer appears and what it says. Nothing
// here is drawn at random -- the button IS the punchline.

// the three controls act one offers
export type ConsentButton = 'accept' | 'reject' | 'manage';

// bar: the notice rewrites itself in place on the bottom edge, saying the
//   content carried here
// modal: accept-all-modal takes the screen and owns its own words, so
//   there is nothing for this table to carry
export type ConsentAnswer = { face: 'modal' } | { face: 'bar'; content: Answer };

export const answers: Record<ConsentButton, ConsentAnswer> = {
	accept: {
		face: 'modal',
	},
	reject: {
		face: 'bar',
		content: {
			body: 'feel good about yourself. You picked the correct option. When I grow up I want to be like you.',
			acceptLabel: null, // when you're the goat I don't deserve your star
			acceptLink: null,
			dismissLabel: 'stay humble, king',
		},
	},
	manage: {
		face: 'bar',
		content: {
			body: "wtf? nobody picks this option. Are you a psychopath? You're trying to tell me that for every website you go to you manage the cookie preferences?",
			acceptLabel: 'Seek help',
			acceptLink: 'https://988lifeline.org',
			dismissLabel: "It's too late for me",
		},
	},
};
