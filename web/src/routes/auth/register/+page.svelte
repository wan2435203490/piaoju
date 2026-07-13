<script lang="ts">
	import { goto } from '$app/navigation';
	import { api, ApiError } from '$lib/api/client';
	import { ERR } from '$lib/api/types';
	import Button from '$lib/components/Button.svelte';
	import FormField from '../FormField.svelte';
	import { saveUser } from '../session';
	import { PASSWORD_MIN, validateEmail, validatePassword, validateRequired } from '../validate';

	let email = $state('');
	let nickname = $state('');
	let password = $state('');
	let touched = $state({ email: false, nickname: false, password: false });
	let submitting = $state(false);
	/** 40901（邮箱已注册）挂在邮箱字段上 */
	let serverEmailError = $state('');
	/** 其余服务端信封错误 → 表单顶部横幅 */
	let formError = $state('');

	const emailError = $derived(
		serverEmailError || (touched.email ? (validateEmail(email) ?? '') : '')
	);
	const nicknameError = $derived(
		touched.nickname ? (validateRequired(nickname, '请输入昵称') ?? '') : ''
	);
	const passwordError = $derived(touched.password ? (validatePassword(password) ?? '') : '');

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		touched = { email: true, nickname: true, password: true };
		serverEmailError = '';
		formError = '';
		if (
			validateEmail(email) ||
			validateRequired(nickname, '请输入昵称') ||
			validatePassword(password)
		) {
			return;
		}

		submitting = true;
		try {
			// register 成功后 client 内部已通过 tokens.ts 持久化 access/refresh
			const data = await api.register({
				email: email.trim(),
				password,
				nickname: nickname.trim()
			});
			saveUser(data.user);
			await goto('/ledger');
		} catch (error) {
			if (error instanceof ApiError && error.code === ERR.EMAIL_TAKEN) {
				serverEmailError = '该邮箱已注册，可直接登录';
			} else if (error instanceof ApiError) {
				formError = error.message;
			} else {
				formError = '注册失败，请稍后重试';
			}
		} finally {
			submitting = false;
		}
	}
</script>

<svelte:head>
	<title>注册 · 拾光票局</title>
</svelte:head>

<h1 class="title">注册</h1>
<p class="subtitle">开一本自己的票据收藏册</p>

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
		oninput={() => (serverEmailError = '')}
	/>

	<FormField
		id="nickname"
		label="昵称"
		bind:value={nickname}
		placeholder="怎么称呼你"
		autocomplete="nickname"
		error={nicknameError}
		onblur={() => (touched.nickname = true)}
	/>

	<FormField
		id="password"
		label="密码"
		type="password"
		bind:value={password}
		placeholder="设置密码"
		autocomplete="new-password"
		hint="至少 {PASSWORD_MIN} 位"
		error={passwordError}
		onblur={() => (touched.password = true)}
	/>

	<Button type="submit" block loading={submitting}>
		{submitting ? '注册中…' : '注册'}
	</Button>
</form>

<p class="alt">已有账号？<a href="/auth/login">去登录</a></p>

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
