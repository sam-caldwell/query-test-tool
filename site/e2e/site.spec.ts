import {test, expect} from '@playwright/test';

test.describe('Site loads without errors', () => {
    test('homepage renders without console errors', async ({page}) => {
        const errors: string[] = [];
        page.on('console', msg => {
            if (msg.type() === 'error') {
                errors.push(msg.text());
            }
        });
        page.on('pageerror', err => {
            errors.push(err.message);
        });

        await page.goto('/');
        await page.waitForTimeout(1000);

        expect(errors).toEqual([]);
    });

    test('page title is set', async ({page}) => {
        await page.goto('/');
        await expect(page).toHaveTitle(/sqlscore/);
    });

    test('sidebar navigation is visible', async ({page}) => {
        await page.goto('/');
        await expect(page.locator('text=Overview')).toBeVisible();
        await expect(page.locator('text=Scoring Rules')).toBeVisible();
        await expect(page.locator('text=Calibration')).toBeVisible();
        await expect(page.locator('text=Installation')).toBeVisible();
        await expect(page.locator('text=Usage')).toBeVisible();
        await expect(page.locator('text=Architecture')).toBeVisible();
        await expect(page.locator('text=Library API')).toBeVisible();
    });

    test('overview content renders', async ({page}) => {
        await page.goto('/');
        await expect(page.locator('h1')).toContainText('sqlscore');
        await expect(page.locator('text=How It Works')).toBeVisible();
    });
});

test.describe('Navigation works', () => {
    test('clicking Scoring Rules navigates', async ({page}) => {
        await page.goto('/');
        await page.click('text=Scoring Rules');
        await expect(page.locator('h1')).toContainText('Scoring Rules');
    });

    test('clicking Calibration navigates', async ({page}) => {
        await page.goto('/');
        await page.click('text=Calibration');
        await expect(page.locator('h1')).toContainText('Weight Calibration');
    });

    test('clicking Installation navigates', async ({page}) => {
        await page.goto('/');
        await page.click('text=Installation');
        await expect(page.locator('h1')).toContainText('Installation');
    });

    test('clicking Usage navigates', async ({page}) => {
        await page.goto('/');
        await page.click('text=Usage');
        await expect(page.locator('h1')).toContainText('Usage');
    });

    test('clicking Architecture navigates', async ({page}) => {
        await page.goto('/');
        await page.click('text=Architecture');
        await expect(page.locator('h1')).toContainText('Architecture');
    });

    test('clicking Library API navigates', async ({page}) => {
        await page.goto('/');
        await page.click('text=Library API');
        await expect(page.locator('h1')).toContainText('Library API');
    });
});

test.describe('SEO', () => {
    test('meta description exists', async ({page}) => {
        await page.goto('/');
        const desc = await page.locator('meta[name="description"]').getAttribute('content');
        expect(desc).toBeTruthy();
        expect(desc).toContain('SQL');
    });

    test('sitemap.xml is accessible', async ({page}) => {
        const response = await page.goto('/sitemap.xml');
        expect(response?.status()).toBe(200);
        const text = await response?.text();
        expect(text).toContain('<urlset');
        expect(text).toContain('query-test-tool.samcaldwell.net');
    });

    test('robots.txt is accessible', async ({page}) => {
        const response = await page.goto('/robots.txt');
        expect(response?.status()).toBe(200);
        const text = await response?.text();
        expect(text).toContain('User-agent');
        expect(text).toContain('Sitemap');
    });

    test('llms.txt is accessible', async ({page}) => {
        const response = await page.goto('/llms.txt');
        expect(response?.status()).toBe(200);
    });
});

test.describe('No runtime errors on any page', () => {
    const pages = ['/', '#/overview', '#/scoring', '#/calibration', '#/installation', '#/usage', '#/architecture', '#/api'];

    for (const path of pages) {
        test(`${path} loads without errors`, async ({page}) => {
            const errors: string[] = [];
            page.on('pageerror', err => errors.push(err.message));

            await page.goto('/' + path);
            await page.waitForTimeout(500);

            expect(errors).toEqual([]);
        });
    }
});
