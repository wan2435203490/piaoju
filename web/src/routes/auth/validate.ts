/**
 * 登录/注册表单的客户端校验（W4 任务卡：邮箱格式 + 密码 ≥8）。
 * 只防手误，最终校验以服务端为准（40001/40103/40901）。
 * 返回值约定：null = 通过；string = 错误文案（直接展示）。
 */

/** 极简邮箱格式：非空 @ 前后段 + 域名含点。不追求 RFC 完备 */
const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

/** PROTOCOL §2：password ≥ 8 */
export const PASSWORD_MIN = 8;

export function validateEmail(email: string): string | null {
	const value = email.trim();
	if (!value) return '请输入邮箱';
	if (!EMAIL_RE.test(value)) return '邮箱格式不正确';
	return null;
}

export function validatePassword(password: string): string | null {
	if (!password) return '请输入密码';
	if (password.length < PASSWORD_MIN) return `密码至少 ${PASSWORD_MIN} 位`;
	return null;
}

export function validateRequired(value: string, message: string): string | null {
	return value.trim() ? null : message;
}
