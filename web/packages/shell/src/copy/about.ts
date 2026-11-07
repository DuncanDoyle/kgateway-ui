/** User-facing copy of the About page (D-063). Each string was picked from five options for plain, accurate wording. */
export const aboutCopy = {
  title: (product: string) => `About ${product}`,
  version: "UI version",
  versionUnknown: "Unknown",
  installs: "Watched installs",
  noInstalls: "No installs found.",
  installsError: "Cannot load installs.",
  controller: "Controller",
  project: "Project",
  source: "Source code",
  docs: "Documentation",
  licence: "Apache License 2.0",
};

/** The upstream project's links; the licence name matches the repository's LICENSE file. */
export const aboutLinks = {
  source: "https://github.com/DuncanDoyle/snellius",
  docs: "https://github.com/DuncanDoyle/snellius#readme",
  licence: "https://github.com/DuncanDoyle/snellius/blob/main/LICENSE",
};
