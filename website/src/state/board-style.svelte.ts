// The board style is which palette the tokens layer serves. It lives on the
// <html> element as data-board-style, put there before the first paint by the
// inline script in BoardLayout -- so this module reads the style already
// showing rather than deciding it a second time. Choosing one stores it, and
// a stored choice outranks the reader's operating system from then on.
const styleKey = 'vulkan-board:style';

export type BoardStyleId = 'classic' | 'night';

export type BoardStyleOption = {
	id: BoardStyleId;
	label: string;
};

// the menu, in the order the footer lists it
export const boardStyles: BoardStyleOption[] = [
	{ id: 'classic', label: 'Vulkan Classic' },
	{ id: 'night', label: 'Vulkan Night' },
];

const defaultStyleId: BoardStyleId = 'classic';

export class BoardStyle {
	current: BoardStyleId = $state(readAppliedStyle());

	select(id: BoardStyleId): void {
		this.current = id;
		applyStyle(id);
		writeStoredStyle(id);
	}
}

export const boardStyle = new BoardStyle();

// on the server there is no element to read; a hand-edited storage key that
// names no style reads as the default, and choosing any style writes the
// attribute back over it
function readAppliedStyle(): BoardStyleId {
	if (typeof document === 'undefined') return defaultStyleId;

	const applied = document.documentElement.dataset['boardStyle'];
	if (applied === undefined) return defaultStyleId;
	const option = boardStyles.find((candidate) => candidate.id === applied);
	return option === undefined ? defaultStyleId : option.id;
}

function applyStyle(id: BoardStyleId): void {
	document.documentElement.dataset['boardStyle'] = id;
}

function writeStoredStyle(id: BoardStyleId): void {
	try {
		window.localStorage.setItem(styleKey, id);
	} catch {
		// storage denied -- the style holds for this page and no longer
	}
}
