import { I18nextProvider } from '@ops/i18n'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { Query } from '../shared/query.js'
import { i18n } from '../i18n.js'
import '../styles.css'
import { ConsoleApp } from './App.js'

createRoot(document.getElementById('root') as HTMLElement).render(
  <StrictMode>
    <I18nextProvider i18n={i18n}>
      <Query>
      <ConsoleApp />
      </Query>
    </I18nextProvider>
  </StrictMode>,
)
