// site-wide constant facts, importable by any component or page
export const repositoryUrl = 'https://github.com/agentstax/vulkan';

// the origin the live site serves from; astro.config reads it too, and
// frozen version deployments fetch /versions.json from it at read time
export const siteUrl = 'https://vulkan-5ss.pages.dev';

// the version this build carries, shown in the version band and compared
// against /versions.json's latest; a release bumps it in the same change
// that adds the release's row to public/versions.json
export const docsVersion = 'main';
