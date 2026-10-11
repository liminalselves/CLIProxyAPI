export const normalizeAutocompleteText = (value: string): string =>
  value.trim().toLowerCase().replace(/（/g, '(').replace(/）/g, ')');

export const matchesAutocompleteQuery = (candidate: string, query: string): boolean =>
  normalizeAutocompleteText(candidate).includes(normalizeAutocompleteText(query));
