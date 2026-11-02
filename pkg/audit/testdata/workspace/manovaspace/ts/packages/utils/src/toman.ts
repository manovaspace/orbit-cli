export interface FormatTomanOptions {
	showUnit?: boolean;
}

export function formatToman(amount: number | string): string {
	return String(amount);
}
