import { useLayoutEffect } from 'react';
import { Outlet } from 'react-router-dom';
import '../styles/northbound-portal-colors.css';

/** Scope the portal palette to this document, including body-mounted dialogs. */
export default function NorthboundPortalTheme() {
  useLayoutEffect(() => {
    const previous = document.body.getAttribute('data-northbound-portal');
    document.body.setAttribute('data-northbound-portal', 'true');
    return () => {
      if (previous === null) document.body.removeAttribute('data-northbound-portal');
      else document.body.setAttribute('data-northbound-portal', previous);
    };
  }, []);

  return <Outlet />;
}
