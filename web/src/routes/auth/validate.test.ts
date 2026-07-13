import { describe, expect, it } from 'vitest';
import { PASSWORD_MIN, validateEmail, validatePassword, validateRequired } from './validate';

describe('validateEmail（W4：邮箱格式客户端校验）', () => {
	it('空值', () => {
		expect(validateEmail('')).toBe('请输入邮箱');
		expect(validateEmail('   ')).toBe('请输入邮箱');
	});

	it('格式不正确', () => {
		expect(validateEmail('leon')).toBe('邮箱格式不正确');
		expect(validateEmail('leon@')).toBe('邮箱格式不正确');
		expect(validateEmail('leon@piaoju')).toBe('邮箱格式不正确');
		expect(validateEmail('leon piaoju@app.com')).toBe('邮箱格式不正确');
	});

	it('合法邮箱（含首尾空白自动 trim）', () => {
		expect(validateEmail('leon@piaoju.app')).toBeNull();
		expect(validateEmail('  leon@piaoju.app  ')).toBeNull();
		expect(validateEmail('a.b+c@x.co')).toBeNull();
	});
});

describe('validatePassword（PROTOCOL §2：≥8 位）', () => {
	it('空值', () => {
		expect(validatePassword('')).toBe('请输入密码');
	});

	it('不足 8 位', () => {
		expect(validatePassword('1234567')).toBe(`密码至少 ${PASSWORD_MIN} 位`);
	});

	it('恰好/超过 8 位', () => {
		expect(validatePassword('12345678')).toBeNull();
		expect(validatePassword('a-very-long-password')).toBeNull();
	});
});

describe('validateRequired', () => {
	it('空白视为空', () => {
		expect(validateRequired('  ', '请输入昵称')).toBe('请输入昵称');
	});

	it('非空通过', () => {
		expect(validateRequired('阿灯', '请输入昵称')).toBeNull();
	});
});
