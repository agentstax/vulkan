// prose structure floor: the body starts at H2 (the H1 is the frontmatter
// title) and heading levels never skip
import remarkFrontmatter from 'remark-frontmatter';
import remarkMdx from 'remark-mdx';
import remarkLintFirstHeadingLevel from 'remark-lint-first-heading-level';
import remarkLintHeadingIncrement from 'remark-lint-heading-increment';

export default {
	plugins: [
		remarkFrontmatter,
		remarkMdx,
		[remarkLintFirstHeadingLevel, 2],
		remarkLintHeadingIncrement,
	],
};
