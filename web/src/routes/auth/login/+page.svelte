<script lang="ts">
	import { goto } from '$app/navigation';
	import { api, ApiError } from '$lib/api/client';
	import { ERR } from '$lib/api/types';
	import Button from '$lib/components/Button.svelte';
	import FormField from '../FormField.svelte';
	import { saveUser } from '../session';
	import { validateEmail, validateRequired } from '../validate';

	let email = $state('');
	let password = $state('');
	/** blur 过 / 提交过才亮字段错误，避免边打字边报错 */
	let touched = $state({ email: false, password: false });
	let submitting = $state(false);
	/** 服务端信封错误（40103 等）→ 表单顶部横幅 */
	let formError = $state('');

	const emailError = $derived(touched.email ? (validateEmail(email) ?? '') : '');
	const passwordError = $derived(
		touched.password ? (validateRequired(password, '请输入密码') ?? '') : ''
	);

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		touched = { email: true, password: true };
		formError = '';
		if (validateEmail(email) || validateRequired(password, '请输入密码')) return;

		submitting = true;
		try {
			// login 成功后 client 内部已通过 tokens.ts 持久化 access/refresh
			const data = await api.login({ email: email.trim(), password });
			saveUser(data.user);
			await goto('/ledger');
		} catch (error) {
			if (error instanceof ApiError && error.code === ERR.BAD_CREDENTIALS) {
				formError = '邮箱或密码错误，请检查后重试';
			} else if (error instanceof ApiError) {
				formError = error.message;
			} else {
				formError = '登录失败，请稍后重试';
			}
		} finally {
			submitting = false;
		}
	}
</script>

<svelte:head>
	<title>登录 · 拾光票局</title>
</svelte:head>

<h1 class="title">登录</h1>
<p class="subtitle">欢迎回来，账本和票夹都还在</p>

<form class="form" onsubmit={submit} novalidate>
	{#if formError}
		<div class="form-error" role="alert">{formError}</div>
	{/if}

	<FormField
		id="email"
		label="邮箱"
		type="email"
		bind:value={email}
		placeholder="you@example.com"
		autocomplete="email"
		error={emailError}
		onblur={() => (touched.email = true)}
	/>

	<FormField
		id="password"
		label="密码"
		type="password"
		bind:value={password}
		placeholder="输入密码"
		autocomplete="current-password"
		error={passwordError}
		onblur={() => (touched.password = true)}
	/>

	<Button type="submit" block loading={submitting}>
		{submitting ? '登录中…' : '登录'}
	</Button>
</form>

<p class="alt">还没有账号？<a href="/auth/register">去注册</a></p>

<a class="skip" href="/ledger">先随便逛逛 →</a>

<style>
	.title {
		font-size: 1.25rem; /* 20 标题 */
		font-weight: 700;
		color: var(--ink);
		margin: 0;
		text-align: center;
	}

	.subtitle {
		font-size: 0.875rem; /* 14 正文 */
		color: var(--ink-2);
		margin: 8px 0 0;
		text-align: center;
	}

	.form {
		display: flex;
		flex-direction: column;
		gap: 16px;
		margin-top: 24px;
	}

	.form-error {
		font-size: 0.875rem; /* 14 正文 */
		color: var(--danger);
		background: color-mix(in srgb, var(--danger) 8%, transparent);
		border: 1px solid color-mix(in srgb, var(--danger) 40%, transparent);
		border-radius: var(--radius-btn);
		padding: 12px;
	}

	.alt {
		margin: 16px 0 0;
		text-align: center;
		font-size: 0.875rem; /* 14 正文 */
		color: var(--ink-2);
	}

	.alt a {
		color: var(--brand);
		font-weight: 600;
		text-decoration: none;
		padding: 12px 4px; /* 触控目标 */
	}

	.skip {
		margin-top: auto;
		text-align: center;
		font-size: 0.875rem;
		color: var(--ink-2);
		text-decoration: none;
		padding: 12px; /* 触控目标 */
	}

	.skip:active {
		color: var(--brand);
	}
</style>
